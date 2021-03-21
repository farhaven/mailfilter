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

func (t *Tree) mergeUp() {
	if t.parent == nil {
		// at the root, can't merge up
		return
	}

	if t.left != nil || t.right != nil {
		// Not yet
		return
	}

	if t.parent.val[0] == t.val[1] {
		if t != t.parent.left {
			panic("invalid parent->child pointer")
		}

		t.parent.val[0] = t.val[0]
		t.parent.left = nil
	}

	if t.parent.val[1] == t.val[0] {
		if t != t.parent.right {
			panic("invalid parent->child pointer")
		}

		t.parent.val[1] = t.val[1]
		t.parent.right = nil
	}

	t.parent.mergeUp()
}

func (t *Tree) add(i interval) {
	if i[1] != i[0]+1 {
		panic(fmt.Sprintf("can't extend %s with %v", t, i))
	}

	if t.val == nil {
		t.val = &i
		return
	}

	// Check if i is already covered by the tree
	if i[0] >= t.val[0] && i[1] <= t.val[1] {
		return
	}

	if i[0] < t.val[0] {
		if t.left == nil {
			t.left = &Tree{
				val:    &i,
				parent: t,
			}

			t.left.mergeUp()

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

			t.right.mergeUp()

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

func (t *Tree) min() *interval {
	if t.left != nil {
		return t.left.min()
	}

	return t.val
}

func (t *Tree) max() *interval {
	if t.right != nil {
		return t.right.max()
	}

	return t.val
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
