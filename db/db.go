// DB implements a simple key-value store for approximate counters. Usually, the counters will be accurate,
// but concurrent modification in different processes may make them a bit inaccurate. This is intented to be
// used as the data store for statistical data where a small error can be traded for increased performance.
//
// It only works on Unix.
package db

import (
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/boltdb/bolt"
)

type mapKey struct {
	bucket string
	key    string
}

type DB struct {
	b *bolt.DB

	mu     sync.RWMutex
	deltas map[mapKey]int
}

// Open opens (and creates, if necessary) a new database. If writeable is false, the
// database is opened in shared, read only mode. Otherwise, it is locked for exclusive
// access and can be modified.
func Open(path string, writeable bool) (*DB, error) {
	fullPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("determining absolute path of %s: %w", path, err)
	}

	b, err := bolt.Open(fullPath, 0600, nil)
	if err != nil {
		return nil, err
	}

	db := &DB{
		b:      b,
		deltas: make(map[mapKey]int),
	}

	return db, nil
}

// Close persists the data in d and closes the database.
func (d *DB) Close() error {
	err := d.Sync()
	if err != nil {
		return fmt.Errorf("can't sync: %w", err)
	}

	return d.b.Close()
}

func (d *DB) Sync() error {
	d.mu.Lock()
	defer d.mu.Unlock()

	err := d.b.Update(func(tx *bolt.Tx) error {
		for mk, delta := range d.deltas {
			b, err := tx.CreateBucketIfNotExists([]byte(mk.bucket))
			if err != nil {
				return err
			}

			raw := b.Get([]byte(mk.key))

			var val int
			if len(raw) != 0 {
				val, err = strconv.Atoi(string(raw))
				if err != nil {
					return err
				}
			}

			val += delta
			if val < 0 {
				val = 0
			}

			err = b.Put([]byte(mk.key), []byte(strconv.Itoa(val)))
			if err != nil {
				return err
			}
		}

		d.deltas = make(map[mapKey]int)

		return nil
	})

	if err != nil {
		return err
	}

	return nil
}

func (d *DB) LogStats() {
	log.Println("TODO: DB stats")
}

// Inc increases the given counter by the given delta. The value stored will be clamped to [0, inf).
func (d *DB) Inc(bucket, key string, delta int) error {
	mk := mapKey{
		bucket: bucket,
		key:    key,
	}

	d.mu.Lock()
	defer d.mu.Unlock()

	d.deltas[mk] += delta

	return nil
}

// Get returns the current value for the given key. If the key does not exist in the given bucket, 0 is returned.
func (d *DB) Get(bucket, key string) (int, error) {
	mk := mapKey{
		bucket: bucket,
		key:    key,
	}

	needSync := false
	d.mu.RLock()
	if d.deltas[mk] != 0 {
		needSync = true
	}
	d.mu.RUnlock()

	if needSync {
		err := d.Sync()
		if err != nil {
			return 0, err
		}
	}

	var val int

	err := d.b.View(func(tx *bolt.Tx) error {
		b := tx.Bucket([]byte(bucket))
		if b == nil {
			return nil
		}

		raw := b.Get([]byte(key))
		if len(raw) == 0 {
			return nil
		}

		var err error

		val, err = strconv.Atoi(string(raw))
		if err != nil {
			return err
		}

		return nil
	})

	return val, err
}
