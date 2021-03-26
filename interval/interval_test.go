package interval

import (
	"math/rand"
	"testing"
)

func TestTree(t *testing.T) {
	intervals := []int{
		7, 2, 1,
		4, 15, 14, 17, 16,
		8, 9,
	}

	expectIntervals := []Interval{
		{1, 2}, {4, 4}, {7, 9}, {14, 17},
	}

	tree := Set{}

	for _, i := range intervals {
		tree.add(i)
	}

	s := tree.slice()
	if len(s) != len(expectIntervals) {
		t.Fatalf("unexpected intervals: want %v, have %v", expectIntervals, s)
	}

	for idx, i := range s {
		if expectIntervals[idx] != i {
			t.Errorf("unexpected interval %v at offset %d, want %v", i, idx, expectIntervals[idx])
		}
	}
}

func BenchmarkSet_addOnly(b *testing.B) {
	rand.Seed(0)

	b.ReportAllocs()

	tree := Set{}

	// Add b.N random intervals to a tree
	for i := 0; i < b.N; i++ {
		start := rand.Intn(8 * 1_000_000)
		tree.add(start)
	}
}

func BenchmarkSet_addAndSlice(b *testing.B) {
	rand.Seed(0)

	b.ReportAllocs()

	tree := Set{}

	// Add b.N random intervals to a tree
	for i := 0; i < b.N; i++ {
		start := rand.Intn(8 * 1_000_000)
		tree.add(start)
	}

	s := tree.slice()
	if len(s) > b.N {
		b.Fatalf("unexpected slice length. want at most %d, have %d", b.N, len(s))
	}
}
