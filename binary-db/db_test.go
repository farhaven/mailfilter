package binary

import (
	"io"
	"testing"
)

func closeFatal(c io.Closer, t *testing.T) {
	err := c.Close()
	if err != nil {
		t.Fatal("unexpected error during close:", err)
	}
}

func TestChunkFile_Open(t *testing.T) {
	cf, err := openChunkFile("test_chunk1")
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	defer closeFatal(cf, t)

	wantN := int64(0)
	if wantN != cf.n {
		t.Errorf("unexpected count in item0: want %d, have %d", wantN, cf.n)
	}
}

func TestChunkFile_Inc(t *testing.T) {
	cf, err := openChunkFile("test_chunk1")
	if err != nil {
		t.Fatal("unexpected error:", err)
	}
	defer closeFatal(cf, t)

	err = cf.Inc("test", 100)
	if err != nil {
		t.Fatal("unexpected error during increase:", err)
	}

	t.Fatal("test me!")
}

func TestChunkFile_Get(t *testing.T) {
	// Generate a bunch of items, insert into DB
	// Close db
	// re-open db
	// get all items, assert that they have the correct value
}

func TestDB_New(t *testing.T) {
	t.Fatal("test me!")
}

func TestDB_Inc(t *testing.T) {
	t.Fatal("test me!")
}

func TestDB_Get(t *testing.T) {
	t.Fatal("test me!")
}

func TestDB_StoreAndLoad(t *testing.T) {
	t.Fatal("test me!")
}
