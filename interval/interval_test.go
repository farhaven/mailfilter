package interval

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestPath(t *testing.T) {
	const leafSize = 3 // 3 entries per leaf/node
	const size = 27

	testCases := []struct {
		in   int
		path []int
	}{
		{1, []int{0, 0, 1}},
		{11, []int{1, 0, 2}},
		{13, []int{1, 1, 1}},
		{14, []int{1, 1, 2}},
		{25, []int{2, 2, 1}},
		{3, []int{0, 1, 0}},
		{5, []int{0, 1, 2}},
		{8, []int{0, 2, 2}},
		{9, []int{1, 0, 0}},
	}

	for _, tc := range testCases {
		t.Run(fmt.Sprintf("%d-%v", tc.in, tc.path), func(t *testing.T) {
			p := derivePath(size, leafSize, tc.in)
			t.Logf("p: %v", p)

			if len(p) != len(tc.path) {
				t.Fatal("unexpected path length", len(p), "want", len(tc.path))
			}

			for i, v := range p {
				if v != tc.path[i] {
					t.Error("want", tc.path[i], "have", v, "at", i)
				}
			}
		})
	}
}

func TestProtoVEB(t *testing.T) {
	p := protoVEB{
		u: leafUniverse * leafUniverse * leafUniverse,
	}

	vals := []int{0, 10, 50, 63, 100, 1000, 4000}

	for _, v := range vals {
		p.set(v, nil)
	}

	t.Logf("p: %s", p)
	t.Logf("s: %v", p.slice())

	for idx, v := range p.slice() {
		if vals[idx] != v {
			t.Errorf("unexpected value %v at %d, want %v", v, idx, vals[idx])
		}
	}
}

func TestProtoVEB_leaf(t *testing.T) {
	p := protoVEB{
		u: leafUniverse,
	}

	testPanic := func(val int) (didPanic bool) {
		defer func() {
			if err := recover(); err != nil {
				didPanic = true
			}
		}()

		p.set(val, nil)

		return didPanic
	}

	expectPanic := []int{-64, -10}

	for _, v := range expectPanic {
		if !testPanic(v) {
			t.Error("expected a panic for", v)
		}
	}

	p.set(1, nil)
	p.set(10, nil)

	if p.summary == 0 {
		t.Error("expected non-zero summary")
	}

	t.Logf("p: %s", p)
	s := p.slice()

	if s[0] != 1 && s[1] != 10 {
		t.Errorf("unexpected slice: %v", s)
	}
}

func TestTree(t *testing.T) {
	intervals := []int{
		7, 2, 1,
		4, 15, 14, 17, 16,
		8, 9,
	}

	expectIntervals := [][2]int{
		{1, 3}, {4, 5}, {7, 10}, {14, 18},
	}

	tree := Tree{
		Max: 64,
	}

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

func BenchmarkTree_add(b *testing.B) {
	rand.Seed(0)

	b.ReportAllocs()

	tree := Tree{
		Max: 8 * 1_000_000,
	}

	// Add b.N random intervals to a tree
	for i := 0; i < b.N; i++ {
		start := rand.Intn(8 * 1_000_000)
		tree.add(start)
	}
}

func BenchmarkTree_addAndSlice(b *testing.B) {
	rand.Seed(0)

	b.ReportAllocs()

	tree := Tree{
		Max: 10 * 10_000_000,
	}

	// Add b.N random intervals to a tree
	for i := 0; i < b.N; i++ {
		start := rand.Intn(8 * 1_000_000)
		tree.add(start)
	}

	s := tree.slice()
	if len(s) > b.N {
		b.Fatalf("unexpected slice length. want at most %d, have %d", b.N, len(s))
	}

	b.Logf("spans: %d", len(s))
}
