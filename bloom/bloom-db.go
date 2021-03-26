package bloom

import (
	"context"
	"encoding/binary"
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

	err = binary.Read(fh, binary.BigEndian, &db.f)
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

	d.mu.RLock()
	err = binary.Write(f, binary.BigEndian, &d.f)
	if err != nil {
		d.mu.RUnlock()
		return fmt.Errorf("marshal filter: %w", err)
	}
	d.mu.RUnlock()

	err = os.Rename(f.Name(), filepath.Join(d.root, d.name))
	if err != nil {
		return fmt.Errorf("renaming temp file: %w", err)
	}

	return nil
}

func (d *DB) Run(ctx context.Context) {
	tick := time.NewTicker(1 * time.Minute)
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

func (d *DB) Add(w []byte, delta uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.f.Add(w, uint64(delta))
	d.dirty = true
}

func (d *DB) Remove(w []byte, delta uint64) {
	d.mu.Lock()
	defer d.mu.Unlock()

	d.f.Remove(w, delta)
	d.dirty = true
}

// Score returns the approximate number of times w has been added to d.
func (d *DB) Score(w []byte) uint64 {
	d.mu.RLock()
	defer d.mu.RUnlock()

	return d.f.Score(w)
}
