package bloom

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/fnv"
)

const (
	filterSize = 16
	numFields  = 8
	cacheSize  = 4096 // maximum cache size
)

type F struct {
	h      hash.Hash32
	fields [numFields][filterSize]uint64
}

func (b *F) Add(w []byte) {
	for i := 0; i < numFields; i++ {
		j := b.hash(i, w)

		b.fields[i][j]++
	}
}

func (b F) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	err := binary.Write(&buf, binary.BigEndian, b.fields)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (b *F) Remove(w []byte) {
	s := b.Score(w)
	if s < 1 {
		// w is probably not in b, no need to remove it
		return
	}

	for i := 0; i < numFields; i++ {
		j := b.hash(i, w)

		// This might happen if we bypassed the check above through a hash collision.
		if b.fields[i][j] == 0 {
			continue
		}

		b.fields[i][j]--
	}
}

// Score returns the approximate number of times w has been added to b. If the result is less
// than one, the word has definitely never been seen by b.
func (b F) Score(w []byte) float64 {
	var s uint64

	for i := 0; i < numFields; i++ {
		j := b.hash(i, w)
		s += b.fields[i][j]
	}

	return float64(s) / float64(numFields)
}

func (b F) String() string {
	return fmt.Sprint(b.fields)
}

func (b *F) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)

	err := binary.Read(buf, binary.BigEndian, &b.fields)
	if err != nil {
		return err
	}

	return nil
}

func (b *F) hash(i int, w []byte) uint32 {
	if b.h == nil {
		b.h = fnv.New32()
	}

	b.h.Reset()
	b.h.Write([]byte{byte(i)})
	b.h.Write(w)

	return b.h.Sum32() % filterSize
}
