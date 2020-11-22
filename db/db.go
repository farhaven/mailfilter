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

type mapKey struct {
	bucket string
	key    string
}

type DB struct {
	path string

	writeable bool
	closed    bool

	lockFH *os.File

	sg singleflight.Group

	dirty sync.Map // Maps mapKey to bool, indicating "dirty" values

	values sync.Map // Maps mapKey to integer values

	mu     sync.Mutex
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

func (d *DB) getID(key string) int {
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

	d.values.Range(func(k, v interface{}) bool {
		mk := k.(mapKey)
		value := v.(int)

		dirtyVal, ok := d.dirty.Load(mk)

		if !ok || !dirtyVal.(bool) {
			return true
		}

		p := mk.bucket + "-" + strconv.Itoa(d.getID(mk.key))

		if partials[p] == nil {
			partials[p] = make(map[string]int)
		}

		partials[p][mk.key] = value

		return true
	})

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

func (d *DB) setInternal(mk mapKey, val int) error {
	if d.closed {
		return ErrClosed
	}

	if !d.writeable {
		return ErrReadonly
	}

	if val == 0 {
		d.values.Delete(mk)
	} else {
		d.values.Store(mk, val)
	}

	d.dirty.Store(mk, true)

	return nil
}

func (d *DB) load(mk mapKey) error {
	p := filepath.Join(d.path, mk.bucket+"-"+strconv.Itoa(d.getID(mk.key)))

	d.mu.Lock()
	if d.loaded[p] {
		d.mu.Unlock()
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
		return err
	}

	m := mVal.(map[string]int)

	d.mu.Lock()
	d.loaded[p] = true
	d.mu.Unlock()

	for k, v := range m {
		mk.key = k

		d.values.Store(mk, v)
	}

	return nil
}

func (d *DB) getInternal(mk mapKey) (int, error) {
	val, ok := d.values.Load(mk)
	if !ok {
		// Not found. Let's try faulting it in.
		// TODO: have d.load return a value indicating whether we need to re-load.
		err := d.load(mk)
		if err != nil {
			return 0, err
		}

		val, ok = d.values.Load(mk)
	}

	if !ok {
		return 0, nil
	}

	return val.(int), nil
}

// Set sets the current value of key to val. It returns an error if the database is
// closed or readonly.
func (d *DB) Set(bucket, key string, val int) error {
	mk := mapKey{
		bucket: bucket,
		key:    key,
	}

	return d.setInternal(mk, val)
}

// Get returns the current value for the given key.
func (d *DB) Get(bucket, key string) (int, error) {
	mk := mapKey{
		bucket: bucket,
		key:    key,
	}

	return d.getInternal(mk)
}
