package tui

import (
	"strings"

	"github.com/alexisiba/asdfbkp/internal/source"
)

type checkState int

const (
	checkNone    checkState = iota // [ ]
	checkAll                       // [x] the whole folder
	checkPartial                   // [~] only part of its contents
)

type node struct {
	name       string
	rel        string
	isDir      bool
	restricted bool
	state      checkState
	parent     *node
	children   []*node
	depth      int

	expanded bool
	loaded   bool
	loading  bool
	err      error
}

// tree is the folder picker. Folders are loaded when opened.
type tree struct {
	root      *node
	nodes     map[string]*node
	cursor    int
	offset    int
	showFiles bool
	// initial is the selection the tree was opened with (a profile). It is
	// used to mark folders that haven't been loaded yet.
	initial []string
}

func newTree(initial []string) *tree {
	root := &node{rel: ".", isDir: true, depth: -1, expanded: true, loading: true}
	root.state = stateFromIncludes(".", initial)
	return &tree{root: root, nodes: map[string]*node{".": root}, initial: initial}
}

// stateFromIncludes computes a path's mark from a selection.
func stateFromIncludes(rel string, includes []string) checkState {
	for _, inc := range includes {
		if inc == "." || inc == rel || strings.HasPrefix(rel, inc+"/") {
			return checkAll
		}
	}
	for _, inc := range includes {
		if rel == "." || strings.HasPrefix(inc, rel+"/") {
			return checkPartial
		}
	}
	return checkNone
}

// setChildren loads a folder's contents. Children inherit the parent's mark.
func (t *tree) setChildren(rel string, entries []source.Entry, err error) {
	n, ok := t.nodes[rel]
	if !ok {
		return
	}
	n.loading, n.loaded, n.err = false, true, err
	if err != nil {
		return
	}
	n.children = nil
	for _, e := range entries {
		c := &node{
			name:       e.Name,
			rel:        source.Join(rel, e.Name),
			isDir:      e.IsDir,
			restricted: e.Restricted,
			parent:     n,
			depth:      n.depth + 1,
		}
		switch {
		case c.restricted:
			c.state = checkNone
		case n.state == checkPartial:
			c.state = stateFromIncludes(c.rel, t.initial)
		default:
			c.state = n.state
		}
		n.children = append(n.children, c)
		t.nodes[c.rel] = c
	}
}

// visible returns the displayed nodes, in order.
func (t *tree) visible() []*node {
	var out []*node
	var walk func(n *node)
	walk = func(n *node) {
		for _, c := range n.children {
			if !c.isDir && !t.showFiles {
				continue
			}
			out = append(out, c)
			if c.expanded {
				walk(c)
			}
		}
	}
	walk(t.root)
	return out
}

func (t *tree) current() *node {
	vis := t.visible()
	if t.cursor < 0 || t.cursor >= len(vis) {
		return nil
	}
	return vis[t.cursor]
}

func (t *tree) move(delta int) {
	t.cursor = max(0, min(t.cursor+delta, len(t.visible())-1))
}

// clampCursor keeps the cursor inside the list after folders are closed or
// files are hidden.
func (t *tree) clampCursor() {
	t.move(0)
}

// collapse closes the current folder or, if already closed, jumps to its parent.
func (t *tree) collapse() {
	n := t.current()
	if n == nil {
		return
	}
	if n.expanded {
		n.expanded = false
		return
	}
	if n.parent == t.root {
		return
	}
	for i, v := range t.visible() {
		if v == n.parent {
			t.cursor = i
			return
		}
	}
}

// toggle marks or unmarks a node together with all of its contents.
func (t *tree) toggle(n *node) {
	if n.restricted {
		return
	}
	next := checkAll
	if n.state == checkAll {
		next = checkNone
	}
	setState(n, next)
	for p := n.parent; p != nil; p = p.parent {
		p.state = aggregate(p)
	}
}

func setState(n *node, s checkState) {
	if n.restricted {
		return
	}
	n.state = s
	for _, c := range n.children {
		setState(c, s)
	}
}

// aggregate computes a folder's mark from its children's. Restricted folders
// don't count because they can't be selected.
func aggregate(n *node) checkState {
	all, none, counted := true, true, 0
	for _, c := range n.children {
		if c.restricted {
			continue
		}
		counted++
		if c.state != checkAll {
			all = false
		}
		if c.state != checkNone {
			none = false
		}
	}
	switch {
	case counted == 0:
		return n.state
	case all:
		return checkAll
	case none:
		return checkNone
	}
	return checkPartial
}

// includes returns the selection as the minimal list of whole paths.
func (t *tree) includes() []string {
	var out []string
	var walk func(n *node)
	walk = func(n *node) {
		switch n.state {
		case checkAll:
			out = append(out, n.rel)
		case checkPartial:
			if !n.loaded {
				// Comes from a profile and was never opened: keep it as is.
				for _, inc := range t.initial {
					if strings.HasPrefix(inc, n.rel+"/") {
						out = append(out, inc)
					}
				}
				return
			}
			for _, c := range n.children {
				walk(c)
			}
		}
	}
	walk(t.root)
	return out
}
