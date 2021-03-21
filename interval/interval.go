package interval

import (
	"fmt"
	"strconv"
)

func derivePath(size, leafSize, input int) []int {
	var l int
	for size > 0 {
		size /= leafSize
		l++
	}
	l--

	res := make([]int, l)

	for i := l - 1; i >= 0; i-- {
		res[i] = input % leafSize
		input /= leafSize
	}

	return res
}

// proto-VEB

const leafUniverse = 64

type protoVEB struct {
	u       int         // Size of the universe represented by this protoVEB (max number)
	cluster []*protoVEB // length: u
	summary uint64      // a bit set for every occupied cluster element
}

func (p protoVEB) String() string {
	return fmt.Sprintf("{u: %v, c: %v, s: %064b}", p.u, p.cluster, p.summary)
}

func (p *protoVEB) set(val int, path []int) {
	if p.u == 0 {
		panic("protoVEB uninitialized")
	}

	if p.u%leafUniverse != 0 {
		panic(fmt.Sprintf("bad universe size %v, must be power of %v", p.u, leafUniverse))
	}

	if val < 0 {
		panic("invalid value: " + strconv.Itoa(val))
	}

	if path == nil {
		path = derivePath(p.u, leafUniverse, val)
	}

	if p.u == leafUniverse {
		p.summary |= (1 << (val % p.u))
		return
	}

	if p.cluster == nil {
		p.cluster = make([]*protoVEB, leafUniverse)
	}

	idx := path[0]

	if p.cluster[idx] == nil {
		p.cluster[idx] = &protoVEB{
			u: p.u / leafUniverse,
		}
	}

	p.cluster[idx].set(val, path[1:])
}

func (p *protoVEB) slice() []int {
	if p.u == leafUniverse {
		res := make([]int, 0, leafUniverse)

		for i := 0; i < leafUniverse; i++ {
			if p.summary&(1<<i) != 0 {
				res = append(res, i)
			}
		}

		return res
	}

	res := make([]int, 0)

	for idx, c := range p.cluster {
		if c == nil {
			continue
		}

		cs := c.slice()

		for _, v := range cs {
			res = append(res, v+(idx*c.u))
		}
	}

	return res
}

type Interval [2]int // Start and End (inclusive)

type Tree struct {
	Max int

	root *protoVEB
}

func (t *Tree) add(val int) {
	if t.root == nil {
		t.init()
	}

	t.root.set(val, nil)
}

func (t *Tree) init() {
	if t.Max == 0 {
		panic("maximum unset")
	}

	universe := leafUniverse

	for universe < t.Max {
		universe *= leafUniverse
	}

	t.root = &protoVEB{
		u: universe,
	}
}

func (t *Tree) slice() []Interval {
	s := t.root.slice()
	if len(s) == 0 {
		return nil
	}

	start := s[0]
	end := s[0]

	var (
		res  []Interval
		rest bool
	)

	for _, e := range s[1:] {
		if e == end+1 {
			end = e
			rest = true
		} else {
			res = append(res, Interval{start, end + 1})
			start = e
			end = e
			rest = false
		}
	}

	if rest {
		res = append(res, Interval{start, end + 1})
	}

	return res
}
