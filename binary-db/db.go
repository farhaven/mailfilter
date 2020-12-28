package binary

import (
	"fmt"
	"io"
	"log"
	"os"

	"github.com/pkg/errors"
)

type SeekerReaderWriterCloser interface {
	io.Reader
	io.Writer

	Seek(offset int64, whence int) (ret int64, err error)
	Close() error
}

// Chunk file layout on Disk:
// Item0 | Item1 | ... | ItemN || Unsorted0 | ... | UnsortedM
// Item0 is a virtual item without a word that has N as the count of sorted items in the chunk file

// Methods on chunkFile are not goroutine safe.
type chunkFile struct {
	n int64 // Number of sorted items in the file

	fh SeekerReaderWriterCloser // Backing file
}

func openChunkFile(path string) (*chunkFile, error) {
	fh, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0600)
	if err != nil {
		return nil, err
	}

	item0, err := newItem(fh)
	if err != nil && !errors.Is(err, io.EOF) {
		return nil, errors.Wrap(err, "reading item0")
	}
	if errors.Is(err, io.EOF) {
		copy(item0.Word[:], "item0")

		// New file, initialize with zero item
		err = item0.Store(fh)
		if err != nil {
			return nil, errors.Wrap(err, "writing initial item0")
		}
	}

	cf := chunkFile{
		n:  item0.Count,
		fh: fh,
	}

	err = cf.compact()
	if err != nil {
		return nil, errors.Wrap(err, "running compaction")
	}

	return &cf, nil
}

func (cf *chunkFile) compact() error {
	// Seek to end-of-file to determine length
	n, err := cf.fh.Seek(0, 2)
	if err != nil {
		return errors.Wrap(err, "seeking to EOF")
	}

	if n == cf.n*itemSize {
		panic(fmt.Sprintf("not running compaction: %d, %d, %d", n, cf.n, itemSize))
	}

	log.Printf("running compaction: %d, %d, %d, %d", n, cf.n, itemSize, (n/itemSize)-1)

	// Load all items from the file, including unsorted items
	items := make(map[[16]byte]int64, (n/itemSize)-1)
	_, err = cf.fh.Seek(itemSize, 0) // Skip item0
	if err != nil {
		return errors.Wrap(err, "seeking to offset of first item")
	}

	n = 0
	for {
		i, err := newItem(cf.fh)
		if err != nil && !errors.Is(err, io.EOF) {
			return errors.Wrapf(err, "reading item %d", n+1)
		}
		if errors.Is(err, io.EOF) {
			break
		}

		items[i.Word] += i.Count
		n++
	}

	log.Println("read", n, "items", items)

	return errors.New("not implemented")
}

func (cf *chunkFile) Get(key string) (int64, error) {
	return 0, errors.New("not implemented")
}

func (cf *chunkFile) Inc(key string, delta int64) error {
	n := 16
	if len(key) < n {
		n = len(key)
	}

	i := item{
		Count: delta,
	}
	copy(i.Word[:], key[:n])

	// Seek to end of file, put new item there
	// TODO: Collect updates and write on close
	_, err := cf.fh.Seek(0, 2)
	if err != nil {
		return errors.Wrap(err, "seeking to end of file")
	}

	err = i.Store(cf.fh)
	if err != nil {
		return errors.Wrap(err, "writing delta item")
	}

	return nil
}

func (cf *chunkFile) Close() error {
	return cf.fh.Close()
}

type DB struct {
}

func (d DB) Get(bucket, key string) (int64, error) {
	return 0, errors.New("not implemented")
}
