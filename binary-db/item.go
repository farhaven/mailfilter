package binary

import (
	"encoding/binary"
	"io"

	"github.com/pkg/errors"
)

const itemSize = 32 // Size of an item in bytes

type item struct {
	Word  [16]byte
	Count int64
	Meta  [8]byte // Currently empty
}

func newItem(r io.Reader) (item, error) {
	i := item{}

	err := binary.Read(r, binary.BigEndian, &i)
	if err != nil {
		return i, errors.Wrap(err, "loading item")
	}

	return i, nil
}

func (i item) Store(w io.Writer) error {
	err := binary.Write(w, binary.BigEndian, i)
	if err != nil {
		return errors.Wrapf(err, "encoding item %v", i)
	}

	return nil
}
