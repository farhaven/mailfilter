// DB implements a simple key-value store for approximate counters. Usually, the counters will be accurate,
// but concurrent modification in different processes may make them a bit inaccurate. This is intented to be
// used as the data store for statistical data where a small error can be traded for increased performance.
//
// It only works on Unix.
package db

import (
	"encoding/gob"
	"errors"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"golang.org/x/sync/errgroup"
	"golang.org/x/sync/singleflight"
	"golang.org/x/sys/unix"
)

var (
	ErrReadonly = errors.New("readonly database")
	ErrClosed   = errors.New("closed database")
)

type DB struct {
	path string

	writeable bool
	closed    bool

	lockFH *os.File

	sg singleflight.Group

	mu     sync.Mutex
	m      map[string]map[string]int
	dirty  map[string]map[string]bool
	loaded map[string]bool // identifies already loaded partial maps, to prevent needless reloads
}

// Open opens (and creates, if necessary) a new database. If writeable is false, the
// database is opened in shared, read only mode. Otherwise, it is locked for exclusive
// access and can be modified.
func Open(path string, writeable bool) (db *DB, err error) {
	fullPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("determining absolute path of %s: %w", path, err)
	}

	db = &DB{
		path:      fullPath,
		writeable: writeable,

		m:      make(map[string]map[string]int),
		dirty:  make(map[string]map[string]bool),
		loaded: make(map[string]bool),
	}

	if writeable {
		err = os.MkdirAll(fullPath, 0750)
		if err != nil {
			return nil, fmt.Errorf("creating database path: %w", err)
		}

		fh, err := os.OpenFile(filepath.Join(path, "lock"), os.O_RDWR|os.O_CREATE, 0755)
		if err != nil {
			return nil, fmt.Errorf("opening database file: %w", err)
		}
		defer func() {
			if err != nil {
				fh.Close()
			}
		}()

		err = unix.Flock(int(fh.Fd()), unix.LOCK_EX)
		if err != nil {
			return nil, fmt.Errorf("locking database: %w", err)
		}

		db.lockFH = fh
	}

	return db, nil
}

func (d *DB) getID(bucket, key string) int {
	const numChunks = 32

	h := fnv.New32()

	_, err := h.Write([]byte(key))
	if err != nil {
		panic(fmt.Errorf("hashing %q: %w", key, err))
	}

	return int(h.Sum32() % numChunks)
}

// Close persists the data in d and closes the database.
func (d *DB) Close() error {
	defer func() {
		if d.writeable {
			d.lockFH.Close()
		}

		d.closed = true
	}()

	// Collect partial maps
	partials := make(map[string]map[string]int)

	for bucket, values := range d.m {
		if d.dirty[bucket] == nil {
			continue
		}

		for key, value := range values {
			if !d.dirty[bucket][key] {
				continue
			}

			p := bucket + "-" + strconv.Itoa(d.getID(bucket, key))

			if partials[p] == nil {
				partials[p] = make(map[string]int)
			}

			partials[p][key] = value
		}
	}

	// Write out partial maps
	var eg errgroup.Group

	for p, m := range partials {
		p := p
		m := m

		eg.Go(func() error {
			tempFH, err := ioutil.TempFile(d.path, p)
			if err != nil {
				return fmt.Errorf("creating temporary file for %q: %w", p, err)
			}
			defer tempFH.Close()

			enc := gob.NewEncoder(tempFH)
			err = enc.Encode(m)
			if err != nil {
				return fmt.Errorf("encoding %q: %w", p, err)
			}

			err = os.Rename(tempFH.Name(), filepath.Join(d.path, p))
			if err != nil {
				return fmt.Errorf("updating %q: %w", p, err)
			}

			return nil
		})
	}

	err := eg.Wait()
	if err != nil {
		return err
	}

	return nil
}

func (d *DB) setLocked(bucket, key string, val int) error {
	if d.closed {
		return ErrClosed
	}

	if !d.writeable {
		return ErrReadonly
	}

	if d.m[bucket] == nil || d.m[bucket][key] == 0 {
		err := d.load(bucket, key)
		if err != nil {
			return err
		}
	}

	if val == 0 {
		delete(d.m[bucket], key)
	} else {
		d.m[bucket][key] = val
	}

	if len(d.m[bucket]) == 0 {
		delete(d.m, bucket)
	}

	if d.dirty[bucket] == nil {
		d.dirty[bucket] = make(map[string]bool)
	}

	d.dirty[bucket][key] = true

	return nil
}

func (d *DB) load(bucket, key string) error {
	// Unlock the db while we're fiddling around with files so that other goroutines can do some useful
	// work while we're loading stuff.
	p := filepath.Join(d.path, bucket+"-"+strconv.Itoa(d.getID(bucket, key)))
	if d.loaded[p] {
		return nil
	}

	d.mu.Unlock()

	mVal, err, _ := d.sg.Do(p, func() (interface{}, error) {
		fh, err := os.Open(p)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return make(map[string]int), nil
			}

			return nil, err
		}
		defer fh.Close()

		var m map[string]int

		dec := gob.NewDecoder(fh)
		err = dec.Decode(&m)
		return m, err
	})

	if err != nil {
		d.mu.Lock()
		return err
	}

	m := mVal.(map[string]int)

	d.mu.Lock()

	d.loaded[p] = true

	if d.m[bucket] == nil {
		d.m[bucket] = m
	} else {
		for k, v := range m {
			d.m[bucket][k] = v
		}
	}

	return nil
}

func (d *DB) getLocked(bucket, key string) (int, error) {
	if d.m[bucket] == nil || d.m[bucket][key] == 0 {
		err := d.load(bucket, key)
		if err != nil {
			return 0, err
		}
	}

	if d.m[bucket] == nil {
		return 0, nil
	}

	return d.m[bucket][key], nil
}

// Set sets the current value of key to val. It returns an error if the database is
// closed or readonly.
func (d *DB) Set(bucket, key string, val int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.setLocked(bucket, key, val)
}

// Inc increases the value for the given key by the given delta. It returns an error
// if the database is closed or readonly.
//
// If clamp is true, the stored value will be clamped to [0, inf)
func (d *DB) Inc(bucket, key string, delta int, clamp bool) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	val, err := d.getLocked(bucket, key)
	if err != nil {
		return err
	}

	val += delta

	if clamp && val < 0 {
		val = 0
	}

	return d.setLocked(bucket, key, val)
}

// Get returns the current value for the given key.
func (d *DB) Get(bucket, key string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.getLocked(bucket, key)
}

func (d *DB) Dump() {
	log.Printf("db: %#v\n", d.m)
}
