package ntuple

import (
	"bytes"
	"errors"
	"io"
	"testing"
)

func TestReader_Next(t *testing.T) {
	inSlice := []byte("abcdefghijklmnopqrstuvwxyz0123456789")
	in := bytes.NewBuffer(inSlice)

	sz := 4

	r := New(in)

	buf := make([]byte, sz)

	var idx int
	for ; ; idx++ {
		err := r.Next(buf)
		if err == io.EOF {
			if idx != len(inSlice)-sz+1 {
				t.Errorf("premature EOF at %d", idx)
			}

			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		t.Logf("idx: %d buf: %s", idx, buf)

		for i, b := range buf {
			if inSlice[idx+i] != b {
				t.Errorf("mismatch at %d: want %v, have %v", idx+i, inSlice[idx+i], b)
			}
		}
	}

	if len(inSlice)-sz+1 != idx {
		t.Errorf("did not consume input buffer: want %d, have %d", len(inSlice)-sz+1, idx)
	}
}

func TestReader_SkipNUL(t *testing.T) {
	inSlice := append([]byte("abc"), []byte{0x00, 0x00}...)
	inSlice = append(inSlice, "def"...)
	inSlice = append(inSlice, 0x00)
	inSlice = append(inSlice, "ghi"...)

	t.Logf("in: %q", inSlice)

	r := New(bytes.NewBuffer(inSlice))

	var seen int
	for ; ; seen++ {
		buf := make([]byte, 2)
		err := r.Next(buf)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %s", err)
		}

		for idx, b := range buf {
			if b == 0x00 {
				t.Errorf("found 0x00 at index %d", idx)
			}
		}
	}

	want := 6
	if want != seen {
		t.Errorf("expected %d chunks, saw %d", want, seen)
	}
}
