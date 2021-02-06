// DB implements a simple key-value store for approximate counters. It is an abstraction atop of a
// key-value store that abstracts away performance specific areas of the KV-store such as repeated
// updates to the same counter.
package db

import (
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/boltdb/bolt"
)

const (
	maxOutstandingDeltas = 20000 // Sync after this many outstanding deltas
	concurrency          = 8     // How many syncers to run
)

type mapKey struct {
	bucket string
	key    string
}

type deltaItem struct {
	mapKey
	d int
}

type DB struct {
	b *bolt.DB

	updates   chan deltaItem
	syncers   sync.WaitGroup
	closeOnce sync.Once
}

// Open opens (and creates, if necessary) a new database. If writeable is false, the
// database is opened in shared, read only mode. Otherwise, it is locked for exclusive
// access and can be modified.
func Open(path string) (*DB, error) {
	fullPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("determining absolute path of %s: %w", path, err)
	}

	b, err := bolt.Open(fullPath, 0600, nil)
	if err != nil {
		return nil, err
	}

	db := &DB{
		b:       b,
		updates: make(chan deltaItem, maxOutstandingDeltas),
	}

	db.syncers.Add(concurrency)
	for i := 0; i < concurrency; i++ {
		go db.syncer()
	}

	return db, nil
}

// Close persists the data in d and closes the database.
func (d *DB) Close() error {
	d.closeOnce.Do(func() {
		close(d.updates)
	})

	d.syncers.Wait()

	return d.b.Close()
}

func (d *DB) String() string {
	stats := struct {
		OutstandingUpdates int
	}{
		OutstandingUpdates: len(d.updates),
	}

	buf, _ := json.Marshal(&stats)
	return string(buf)
}

func (d *DB) syncer() {
	defer d.syncers.Done()

	for {
		// Wait for first update or channel close
		first, ok := <-d.updates
		if !ok {
			log.Println("syncer work channel closed")
			return
		}

		updates := make(map[mapKey]int)
		updates[first.mapKey] = first.d

		for n := 0; n < maxOutstandingDeltas && len(updates) < maxOutstandingDeltas/2; n++ {
			// Try to get another delta non-blocking
			select {
			case item, ok := <-d.updates:
				if !ok {
					break
				}

				updates[item.mapKey] += item.d
			default:
				break // Nothing on the queue right now
			}
		}

		// Got a map of updates now, let's apply them to the on-disk DB
		err := d.b.Batch(func(tx *bolt.Tx) error {
			for mk, delta := range updates {
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

			return nil
		})

		if err != nil {
			log.Printf("can't sync %d updates: %s", len(updates), err) // TODO: Retry
		}
	}
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

	d.updates <- deltaItem{
		mapKey: mk,
		d:      delta,
	}

	return nil
}

// Get returns the current value for the given key. If the key does not exist in the given bucket, 0 is returned.
func (d *DB) Get(bucket, key string) (int, error) {
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
