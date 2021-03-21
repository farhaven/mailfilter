package interval

import (
	"math/rand"
	"testing"
)

func TestTree(t *testing.T) {
	intervals := [][2]int{
		{7, 8}, {2, 3}, {1, 2},
		{4, 5}, {15, 16}, {14, 15}, {17, 18}, {16, 17},
		{8, 9}, {9, 10},
	}

	expectIntervals := [][2]int{
		{1, 3}, {4, 5}, {7, 10}, {14, 18},
	}

	tree := Tree{}

	t.Logf("tree: %v", tree)

	for _, i := range intervals {
		tree.add(i)
		t.Logf("%v: tree: %v", i, tree)
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

func TestTree_depth(t *testing.T) {
	tree := Tree{}

	expectDepth := func(e int) {
		d := tree.depth()
		if e != d {
			t.Errorf("expected depth %v, have %v", e, d)
		}
	}

	expectDepth(0)

	tree.add(interval{5, 6})
	expectDepth(1)

	tree.add(interval{4, 5})
	expectDepth(2)

	tree.add(interval{3, 4})
	expectDepth(3)

	tree.add(interval{6, 7})
	expectDepth(3)
}

func TestTree_failOddInterval(t *testing.T) {
	tree := Tree{}

	var didPanic bool

	test := func() {
		defer func() {
			if err := recover(); err != nil {
				didPanic = true
			}
		}()

		tree.add(interval{1, 100})
	}

	test()

	if !didPanic {
		t.Errorf("expected a panic, got nothing: %s", tree)
	}
}

func BenchmarkTree_add(b *testing.B) {
	rand.Seed(0)

	b.ReportAllocs()

	tree := Tree{}

	// Add b.N random intervals to a tree
	for i := 0; i < b.N; i++ {
		start := rand.Int()
		val := interval{start, start + 1}
		tree.add(val)
	}

	b.ReportMetric(float64(tree.depth()), "depth")
}

func BenchmarkTree_addAndSlice(b *testing.B) {
	rand.Seed(0)

	b.ReportAllocs()

	tree := Tree{}

	// Add b.N random intervals to a tree
	for i := 0; i < b.N; i++ {
		start := rand.Int()
		val := interval{start, start + 1}
		tree.add(val)
	}

	s := tree.slice()
	if len(s) > b.N {
		b.Fatalf("unexpected slice length. want at most %d, have %d", b.N, len(s))
	}

	b.ReportMetric(float64(tree.depth()), "depth")
}
