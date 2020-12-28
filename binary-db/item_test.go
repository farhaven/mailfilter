package binary

import (
	"bytes"
	"testing"
)

func TestItem_StoreAndLoad(t *testing.T) {
	i := item{
		Count: 27,
	}
	copy(i.Word[:], "foo")

	t.Logf("item: %#v", i)

	var buf bytes.Buffer
	err := i.Store(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if itemSize != buf.Len() {
		t.Errorf("unexpected buffer size: want %d, have %d", itemSize, buf.Len())
	}

	itemLoaded, err := newItem(&buf)
	if err != nil {
		t.Fatalf("unexpected error: %s", err)
	}

	if i != itemLoaded {
		t.Errorf("items not equal. expected %#v, got %#v", i, itemLoaded)
	}
}
