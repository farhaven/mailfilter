package bloom

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash"
	"hash/fnv"
	"math"
)

const (
	filterSize = 1_000_000
	numFuncs   = 8
)

type F struct {
	h     hash.Hash32
	buf   [1]byte
	field [numFuncs][filterSize]uint64
}

func (b *F) Add(w []byte) {
	for i := byte(0); i < numFuncs; i++ {
		j := b.hash(i, w)

		b.field[i][j]++
	}
}

func (b *F) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	err := binary.Write(&buf, binary.BigEndian, b.field)
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

	for i := byte(0); i < numFuncs; i++ {
		j := b.hash(i, w)

		// This might happen if we bypassed the check above through a hash collision.
		if b.field[i][j] == 0 {
			continue
		}

		b.field[i][j]--
	}
}

// Score returns the approximate number of times w has been added to b.
func (b *F) Score(w []byte) uint64 {
	var s uint64 = math.MaxUint64

	for i := byte(0); i < numFuncs; i++ {
		j := b.hash(i, w)
		if s > b.field[i][j] {
			s = b.field[i][j]
		}
	}

	return s
}

func (b F) String() string {
	return fmt.Sprint(b.field)
}

func (b *F) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)

	err := binary.Read(buf, binary.BigEndian, &b.field)
	if err != nil {
		return err
	}

	return nil
}

func (b *F) hash(i byte, w []byte) uint32 {
	if b.h == nil {
		b.h = fnv.New32()
	}

	b.buf[0] = i

	b.h.Reset()
	b.h.Write(b.buf[:])
	b.h.Write(w)

	return b.h.Sum32() % filterSize
}
