// DB implements a simple key-value store for approximate counters. Usually, the counters will be accurate,
// but concurrent modification in different processes may make them a bit inaccurate. This is intented to be
// used as the data store for statistical data where a small error can be traded for increased performance.
//
// It only works on Unix.
package db

import (
	"errors"
	"fmt"
	"hash/fnv"
	"io/ioutil"
	"os"
	"path/filepath"
	"strconv"
	"sync"

	jsoniterator "github.com/json-iterator/go"
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

	mu     sync.Mutex
	values map[mapKey]int  // Maps mapKey to integer values, used as a cache
	deltas map[mapKey]int  // Stores deltas to values, persisted upon close
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

		values: make(map[mapKey]int),
		deltas: make(map[mapKey]int),

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
	d.mu.Lock()
	defer d.mu.Unlock()

	defer func() {
		if d.writeable {
			d.lockFH.Close()
		}

		d.closed = true
	}()

	// Collect partial maps
	partials := make(map[string]map[string]int)

	for mk, value := range d.deltas {
		p := mk.bucket + "-" + strconv.Itoa(d.getID(mk.key))

		if partials[p] == nil {
			partials[p] = make(map[string]int)
		}

		partials[p][mk.key] = value
	}

	// Write out partial maps
	var eg errgroup.Group

	for p, m := range partials {
		p := p
		m := m

		eg.Go(func() error {
			fp := filepath.Join(d.path, p)

			fh, err := os.OpenFile(fp, os.O_RDWR|os.O_APPEND|os.O_CREATE, 0600)
			if err != nil {
				return fmt.Errorf("opening partial file %q: %w", p, err)
			}
			defer fh.Close()

			enc := jsoniterator.NewEncoder(fh)
			err = enc.Encode(m)
			if err != nil {
				return fmt.Errorf("encoding %q: %w", p, err)
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

func (d *DB) load(mk mapKey) error {
	p := filepath.Join(d.path, mk.bucket+"-"+strconv.Itoa(d.getID(mk.key)))

	if d.loaded[p] {
		return nil
	}

	// we enter here locked, so unlock while doing work
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

		dec := jsoniterator.NewDecoder(fh)
		res := make(map[string]int)

		numChunks := 0
		for dec.More() {
			var m map[string]int

			err = dec.Decode(&m)
			if err != nil {
				return nil, err
			}

			for k, v := range m {
				res[k] += v
				if res[k] < 0 {
					res[k] = 0
				}
			}

			numChunks++
		}

		for k, v := range res {
			if v == 0 {
				delete(res, k)
			}
		}

		// Rewrite partial so that we have a single large chunk
		if numChunks > 1 {
			tempFH, err := ioutil.TempFile(d.path, "")
			if err != nil {
				return nil, err
			}
			defer tempFH.Close()

			enc := jsoniterator.NewEncoder(tempFH)

			err = enc.Encode(res)
			if err != nil {
				return nil, err
			}

			err = os.Rename(tempFH.Name(), p)
			if err != nil {
				return nil, err
			}
		}

		return res, nil
	})

	// Restore lock state
	d.mu.Lock()

	if err != nil {
		return err
	}

	m := mVal.(map[string]int)

	d.loaded[p] = true

	for k, v := range m {
		mk.key = k

		d.values[mk] = v
	}

	return nil
}

func (d *DB) incInternal(mk mapKey, delta int) error {
	if d.closed {
		return ErrClosed
	}

	if !d.writeable {
		return ErrReadonly
	}

	d.deltas[mk] += delta

	return nil
}

func (d *DB) getInternal(mk mapKey) (int, error) {
	err := d.load(mk)
	if err != nil {
		return 0, err
	}

	val := d.values[mk] + d.deltas[mk]
	if val < 0 {
		val = 0
	}

	return val, nil
}

// Inc increases the given counter by the given delta. The value stored will be clamped to [0, inf).
func (d *DB) Inc(bucket, key string, delta int) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	mk := mapKey{
		bucket: bucket,
		key:    key,
	}

	return d.incInternal(mk, delta)
}

// Get returns the current value for the given key.
func (d *DB) Get(bucket, key string) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()

	mk := mapKey{
		bucket: bucket,
		key:    key,
	}

	return d.getInternal(mk)
}
