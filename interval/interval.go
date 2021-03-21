package interval

import "fmt"

type interval [2]int

type Tree struct {
	val *interval

	left   *Tree
	right  *Tree
	parent *Tree
}

func (t Tree) String() string {
	return fmt.Sprintf("{val: %v, left: %s, right: %s}", t.val, t.left, t.right)
}

func (t *Tree) add(i interval) {
	if i[1] != i[0]+1 {
		panic(fmt.Sprintf("can't extend %s with %v", t, i))
	}

	if t.val == nil {
		t.val = &i
		return
	}

	if i[0] < t.val[0] {
		if t.left == nil {
			t.left = &Tree{
				val:    &i,
				parent: t,
			}

			return
		}

		t.left.add(i)
		return
	}

	if i[0] >= t.val[1] {
		if t.right == nil {
			t.right = &Tree{
				val:    &i,
				parent: t,
			}

			return
		}

		t.right.add(i)
		return
	}

	panic(fmt.Sprintf("don't know how to add %v to %s", i, t))
}

func (t Tree) depth() int {
	if t.val == nil {
		return 0
	}

	var (
		ldepth int
		rdepth int
	)

	if t.left != nil {
		ldepth = t.left.depth()
	}

	if t.right != nil {
		rdepth = t.right.depth()
	}

	if ldepth > rdepth {
		return 1 + ldepth
	}

	return 1 + rdepth
}

func (t Tree) slice() []interval {
	if t.val == nil {
		return nil
	}

	var res []interval

	if t.left != nil {
		res = t.left.slice()
	}

	res = append(res, *t.val)

	if t.right != nil {
		res = append(res, t.right.slice()...)
	}

	if t.parent == nil {
		compressed := make([]interval, 1, len(res))

		compressed[0] = res[0]

		for _, v := range res[1:] {
			if v[0] == compressed[len(compressed)-1][1] {
				compressed[len(compressed)-1][1] = v[1]
			} else {
				compressed = append(compressed, v)
			}
		}

		return compressed
	}

	return res
}
