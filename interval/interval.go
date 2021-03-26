package interval

import "sort"

type Interval struct {
	Start int
	End   int // inclusive
}

type token struct{}

type Set struct {
	m map[int]token
}

func (t *Set) add(val int) {
	if t.m == nil {
		t.m = make(map[int]token)
	}

	t.m[val] = token{}
}

func (t *Set) slice() []Interval {
	if len(t.m) == 0 {
		return nil
	}

	var indices []int
	for i := range t.m {
		indices = append(indices, i)
	}

	sort.Ints(indices)

	var intervals []Interval

	current := Interval{
		Start: indices[0],
		End:   indices[0],
	}

	for _, i := range indices[1:] {
		if current.End == i-1 {
			// We can extend the current interval
			current.End = i
			continue
		}

		intervals = append(intervals, current)
		current = Interval{
			Start: i,
			End:   i,
		}
	}

	if len(intervals) == 0 || intervals[len(intervals)-1] != current {
		intervals = append(intervals, current)
	}

	return intervals
}
