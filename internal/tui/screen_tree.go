package tui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const appDataNotice = "Android/data and Android/obb hold apps' private data and are never backed up."

// treeRows is how many tree rows fit on screen.
func (m Model) treeRows() int {
	if m.height == 0 {
		return 15
	}
	// Header, padding, title, notice and footer.
	return max(m.height-12, 5)
}

func (m Model) updateTree(key string) (tea.Model, tea.Cmd) {
	t := m.tree
	var cmd tea.Cmd
	switch key {
	case "q":
		return m, tea.Quit
	case "esc", "backspace":
		m.screen = screenMode
		return m, nil
	case "up", "k":
		t.move(-1)
	case "down", "j":
		t.move(1)
	case "pgup":
		t.move(-m.treeRows())
	case "pgdown":
		t.move(m.treeRows())
	case "home", "g":
		t.cursor = 0
	case "end", "G":
		t.cursor = max(len(t.visible())-1, 0)
	case "right", "l":
		cmd = m.expand(t.current())
	case "left", "h":
		t.collapse()
	case " ":
		if n := t.current(); n != nil {
			if n.restricted {
				m.notice = appDataNotice
			}
			t.toggle(n)
		}
	case "a":
		t.toggle(t.root)
	case "f":
		t.showFiles = !t.showFiles
		t.clampCursor()
	case "enter":
		inc := t.includes()
		if len(inc) == 0 {
			m.notice = "Mark at least one folder with the space bar."
			return m, nil
		}
		return m.enterSummary(inc)
	}
	m.scrollTree()
	return m, cmd
}

// expand opens a folder and, the first time, requests its contents.
func (m *Model) expand(n *node) tea.Cmd {
	if n == nil || !n.isDir {
		return nil
	}
	if n.restricted {
		m.notice = appDataNotice
		return nil
	}
	n.expanded = true
	// If reading failed, opening it again retries.
	if n.loading || n.loaded && n.err == nil {
		return nil
	}
	n.loading, n.err = true, nil
	return listCmd(m.ctx, m.src, n.rel)
}

// scrollTree scrolls the view so the cursor is always visible. Rows can span
// several lines when long names wrap, so it counts lines, not rows.
func (m *Model) scrollTree() {
	t, budget, w := m.tree, m.treeRows(), m.contentWidth()
	vis := t.visible()
	t.offset = max(0, min(t.offset, len(vis)-1))
	if t.cursor < t.offset {
		t.offset = t.cursor
	}
	for t.offset < t.cursor {
		used := 0
		for i := t.offset; i <= t.cursor; i++ {
			used += lipgloss.Height(m.treeLine(vis[i], i == t.cursor, w))
		}
		if used <= budget {
			break
		}
		t.offset++
	}
}

func (m Model) viewTree() string {
	t, w, budget := m.tree, m.contentWidth(), m.treeRows()
	var b strings.Builder

	sel := "nothing"
	if inc := t.includes(); len(inc) > 0 {
		total, ok := m.totalSize(inc)
		sel = humanSize(total)
		if !ok {
			sel += "…"
		}
	}
	selected := dim.Render("Selected: ") + selectedStyle.Render(sel)
	b.WriteString(row(w, col{s: "Choose what to back up"}, col{w: lipgloss.Width(selected) + 2, s: selected, right: true}) + "\n\n")

	vis := t.visible()
	switch {
	case t.root.loading:
		b.WriteString(wrap(m.spinner.View()+" Reading "+safe(m.src.Root())+"...", w) + "\n")
		budget--
	case t.root.err != nil:
		b.WriteString(wrap(warn.Render("Could not read the device: "+safe(t.root.err.Error())), w) + "\n")
		budget--
	case len(vis) == 0:
		b.WriteString(wrap(dim.Render("No folders here. Press f to show files."), w) + "\n")
		budget--
	}
	used := 0
	for i := t.offset; i < len(vis); i++ {
		line := m.treeLine(vis[i], i == t.cursor, w)
		h := lipgloss.Height(line)
		if used+h > budget && i > t.offset {
			break
		}
		b.WriteString(line + "\n")
		used += h
	}
	// Padding so the screen height doesn't change when folders open.
	b.WriteString(strings.Repeat("\n", max(budget-used, 0)))

	if m.notice != "" {
		b.WriteString(wrap(warn.Render(safe(m.notice)), w))
	}
	b.WriteString("\n")
	b.WriteString(footer(w, "↑↓ move", "→/← open/close", "space mark", "a all",
		filesKey(t.showFiles), "enter next", "esc back"))
	return b.String()
}

func filesKey(shown bool) string {
	if shown {
		return "f hide files"
	}
	return "f show files"
}

func (m Model) treeLine(n *node, selected bool, w int) string {
	cur := "  "
	if selected {
		cur = accent.Render("› ")
	}
	arrow := "  "
	if n.isDir {
		arrow = "▸ "
		if n.expanded {
			arrow = "▾ "
		}
	}
	prefix := cur + checkbox(n) + " " + strings.Repeat("  ", n.depth) + arrow

	var right string
	switch {
	case n.restricted:
		right = dim.Render("excluded")
	case n.err != nil:
		right = warn.Render("read error")
	case n.loading:
		right = dim.Render("loading…")
	default:
		if s, ok := m.sizes[n.rel]; ok {
			switch {
			case !s.done:
				right = dim.Render("measuring…")
			case s.err != nil:
				right = warn.Render("?")
			default:
				right = dim.Render(humanSize(s.bytes))
			}
		}
	}

	name := safe(n.name)
	switch {
	case n.restricted:
		name = dim.Render(name)
	case selected:
		name = selectedStyle.Render(name)
	}
	return row(w,
		col{w: lipgloss.Width(prefix), s: prefix},
		col{s: name},
		col{w: sizeColWidth, s: right, right: true},
	)
}

// sizeColWidth fits the longest size label, "measuring…", plus a gap.
const sizeColWidth = 13

func checkbox(n *node) string {
	switch {
	case n.restricted:
		return dim.Render("[-]")
	case n.state == checkAll:
		return accent.Render("[x]")
	case n.state == checkPartial:
		return warn.Render("[~]")
	}
	return dim.Render("[ ]")
}

// enterSummary moves to the summary with the given selection.
func (m Model) enterSummary(includes []string) (tea.Model, tea.Cmd) {
	m.includes = includes
	m.screen = screenSummary
	m.check = m.inspectDest()
	m.prepareSave()
	return m, m.requestSizes(includes...)
}

func describeIncludes(inc []string) string {
	if len(inc) == 1 && inc[0] == "." {
		return "the whole device"
	}
	noun := "items"
	if len(inc) == 1 {
		noun = "item"
	}
	return fmt.Sprintf("%d %s: %s", len(inc), noun, summarizeIncludes(inc))
}
