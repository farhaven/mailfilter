package bloom

import (
	"fmt"
	"math"
)

const (
	filterSize = 1_000_000
	numFuncs   = 8
)

type F struct {
	Field [numFuncs][filterSize]uint64
}

func (b *F) Add(w []byte, delta uint64) {
	for i := uint32(0); i < numFuncs; i++ {
		j := b.hash(i, w)

		b.Field[i][j] += delta
	}
}

func (b *F) Remove(w []byte, delta uint64) {
	s := b.Score(w)
	if s == 0 {
		// w is probably not in b, no need to remove it
		return
	}

	if s < delta {
		delta = s
	}

	for i := uint32(0); i < numFuncs; i++ {
		j := b.hash(i, w)

		// This might happen if we bypassed the check above through a hash collision.
		if b.Field[i][j] < delta {
			continue
		}

		b.Field[i][j] -= delta
	}
}

// Score returns the approximate number of times w has been added to b.
func (b *F) Score(w []byte) uint64 {
	var s uint64 = math.MaxUint64

	for i := uint32(0); i < numFuncs; i++ {
		j := b.hash(i, w)
		if s > b.Field[i][j] {
			s = b.Field[i][j]
		}
	}

	return s
}

func (b F) String() string {
	return fmt.Sprint(b.Field)
}

// Inlined FNV32

const (
	offset32 = 2166136261
	prime32  = 16777619
)

func (b *F) hash(i uint32, w []byte) uint32 {
	var s uint32 = offset32

	s *= prime32
	s ^= i

	for _, c := range w {
		s *= prime32
		s ^= uint32(c)
	}

	return s % filterSize
}
