package bloom

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"
)

type DB struct {
	root string
	name string

	mu sync.RWMutex

	dirty bool
	f     F
}

func NewDB(root, name string) (*DB, error) {
	db := &DB{
		root: root,
		name: name,
	}

	fp := filepath.Join(root, name)

	var perr *os.PathError

	fh, err := os.Open(fp)
	if errors.As(err, &perr) {
		return db, nil
	}
	if err != nil {
		return nil, err
	}
	defer fh.Close()

	buf, err := ioutil.ReadAll(fh)
	if err != nil {
		return nil, err
	}

	err = db.f.UnmarshalBinary(buf)
	if err != nil {
		return nil, err
	}

	return db, nil
}

func (d *DB) persist() error {
	f, err := ioutil.TempFile(d.root, "*")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer f.Close()

	buf, err := d.f.MarshalBinary()
	if err != nil {
		return fmt.Errorf("marshal filter: %w", err)
	}

	_, err = f.Write(buf)
	if err != nil {
		return fmt.Errorf("writing filter: %w", err)
	}

	err = os.Rename(f.Name(), filepath.Join(d.root, d.name))
	if err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}

func (d *DB) Run(ctx context.Context) {
	tick := time.NewTicker(5 * time.Second)
	done := false

	for !done {
		select {
		case <-ctx.Done():
			// Persist one last time, then quit
			done = true
			tick.Stop()
		case <-tick.C:
		}

		// Persist DB
		if !d.dirty {
			continue
		}

		log.Println("persisting updates")

		err := d.persist()
		if err != nil {
			log.Println("failed to persist:", err)
			continue
		}

		d.dirty = false
	}
}

func (d *DB) Add(w []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.f.Add(w)
	d.dirty = true
}

func (d *DB) Remove(w []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.f.Remove(w)
}

// Score returns the approximate number of times w has been added to d.
func (d *DB) Score(w []byte) uint64 {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.f.Score(w)
}
