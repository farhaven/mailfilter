// DB implements a simple key-value store, with strings as keys and integers as values.
// If a key has no value, it is assumed to be zero and keeps the whole data set in
// memory.
//
// It only works on Unix.
package db

import (
	"encoding/gob"
	"errors"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"path/filepath"
	"sync"

	"golang.org/x/sys/unix"
)

var (
	ErrReadonly = errors.New("readonly database")
	ErrClosed   = errors.New("closed database")
)

type DB struct {
	path string

	// fh needs to be held open even after loading the db because the file lock is attached to it.
	fh *os.File

	writeable bool
	closed    bool

	mu sync.Mutex
	m  map[string]map[string]int
}

// Open opens (and creates, if necessary) a new database. If writeable is false, the
// database is opened in shared, read only mode. Otherwise, it is locked for exclusive
// access and can be modified.
func Open(file string, writeable bool) (*DB, error) {
	flags := os.O_RDWR | os.O_CREATE
	if !writeable {
		flags = os.O_RDONLY
	}

	fh, err := os.OpenFile(file, flags, 0755)
	if err != nil {
		return nil, fmt.Errorf("opening database file: %w", err)
	}

	if writeable {
		err = unix.Flock(int(fh.Fd()), unix.LOCK_EX)
		if err != nil {
			fh.Close()
			return nil, fmt.Errorf("locking database: %w", err)
		}
	}

	fpath, err := filepath.Abs(file)
	if err != nil {
		fh.Close()
		return nil, fmt.Errorf("determining absolute path of %s: %w", file, err)
	}

	db := DB{
		path:      fpath,
		writeable: writeable,
		fh:        fh,
		m:         make(map[string]map[string]int),
	}

	dec := gob.NewDecoder(fh)
	err = dec.Decode(&db.m)
	if err != nil && !writeable {
		fh.Close()
		return nil, fmt.Errorf("can't decode database: %w", err)
	}

	return &db, nil
}

// Close persists the data in d and closes the database.
func (d *DB) Close() error {
	defer func() {
		d.closed = true
	}()

	if !d.writeable {
		return d.fh.Close()
	}

	tempFH, err := ioutil.TempFile(filepath.Dir(d.path), "mailfilter")
	if err != nil {
		return fmt.Errorf("creating temp file: %w", err)
	}
	defer tempFH.Close()

	enc := gob.NewEncoder(tempFH)
	err = enc.Encode(d.m)
	if err != nil {
		return fmt.Errorf("encoding database: %w", err)
	}

	err = os.Rename(tempFH.Name(), d.path)
	if err != nil {
		return fmt.Errorf("updating database: %w", err)
	}

	return d.fh.Close()
}

func (d *DB) setLocked(bucket, key string, val int) error {
	if d.closed {
		return ErrClosed
	}

	if !d.writeable {
		return ErrReadonly
	}

	if d.m[bucket] == nil {
		d.m[bucket] = make(map[string]int)
	}

	if val == 0 {
		delete(d.m[bucket], key)
	} else {
		d.m[bucket][key] = val
	}

	if len(d.m[bucket]) == 0 {
		delete(d.m, bucket)
	}

	return nil
}

func (d *DB) getLocked(bucket, key string) int {
	if d.m[bucket] == nil {
		return 0
	}

	return d.m[bucket][key]
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

	val := d.getLocked(bucket, key) + delta
	if clamp && val < 0 {
		val = 0
	}

	return d.setLocked(bucket, key, val)
}

// Get returns the current value for the given key.
func (d *DB) Get(bucket, key string) int {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.getLocked(bucket, key)
}

func (d *DB) Dump() {
	log.Printf("db: %#v\n", d.m)
}
