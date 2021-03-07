package bloom

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"hash/fnv"
)

const (
	filterSize = 16
	numFields  = 8
	cacheSize  = 4096 // maximum cache size
)

type hashKey struct {
	i int
	w string
}

type Bloom struct {
	hashes map[hashKey]uint32
	fields [numFields][filterSize]uint64
}

func (b Bloom) MarshalBinary() ([]byte, error) {
	var buf bytes.Buffer

	err := binary.Write(&buf, binary.BigEndian, b.fields)
	if err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

func (b *Bloom) UnmarshalBinary(data []byte) error {
	buf := bytes.NewBuffer(data)

	err := binary.Read(buf, binary.BigEndian, &b.fields)
	if err != nil {
		return err
	}

	return nil
}

func (b Bloom) String() string {
	return fmt.Sprint(b.fields)
}

func (b *Bloom) hash(i int, w string) uint32 {
	if b.hashes == nil || len(b.hashes) == cacheSize {
		b.hashes = make(map[hashKey]uint32)
	}

	k := hashKey{
		i: i,
		w: w,
	}

	s, ok := b.hashes[k]
	if ok {
		return s
	}

	h := fnv.New32()

	h.Write([]byte{byte(i)})
	h.Write([]byte(w))

	b.hashes[k] = h.Sum32() % filterSize

	return b.hashes[k]
}

func (b *Bloom) add(w string) {
	for i := 0; i < numFields; i++ {
		j := b.hash(i, w)

		b.fields[i][j]++
	}
}

func (b Bloom) score(w string) float64 {
	var s uint64

	for i := 0; i < numFields; i++ {
		j := b.hash(i, w)
		s += b.fields[i][j]
	}

	return float64(s) / float64(numFields)
}

func (b *Bloom) remove(w string) {
	s := b.score(w)
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
