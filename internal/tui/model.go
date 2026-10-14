// Package tui implements asdfbkp's terminal user interface.
package tui

import (
	"context"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/progress"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/alexisiba/asdfbkp/internal/backup"
	"github.com/alexisiba/asdfbkp/internal/dest"
	"github.com/alexisiba/asdfbkp/internal/device"
	"github.com/alexisiba/asdfbkp/internal/profile"
	"github.com/alexisiba/asdfbkp/internal/source"
)

const (
	scanTimeout = 15 * time.Second // adb can be slow when it has to start its server
	listTimeout = 30 * time.Second
	sizeTimeout = 10 * time.Minute
)

// sizeSlots limits concurrent size computations so neither the disk nor the
// ADB connection gets saturated.
var sizeSlots = make(chan struct{}, 4)

type screen int

const (
	screenScanning screen = iota
	screenDevices
	screenHelp
	screenMode
	screenTree
	screenSummary
	screenCopy
	screenDone
)

// Options configures the TUI.
type Options struct {
	Version  string
	Dest     string         // base backup folder (argument or current directory)
	Profiles *profile.Store // nil when profiles are disabled
	// ProfileWarning explains why profiles could not be loaded.
	ProfileWarning string
}

type Model struct {
	opts     Options
	screen   screen
	helpFrom screen // screen the help returns to
	spinner  spinner.Model
	width    int
	height   int
	notice   string

	// Devices
	result    device.Result
	devCursor int

	// Backup being prepared. ctx is cancelled when leaving the device, to stop
	// computations still running in the background.
	ctx        context.Context
	cancel     context.CancelFunc
	dev        device.Device
	src        source.Source
	modeCursor int
	mode       modeKind
	tree       *tree
	includes   []string
	sizes      map[string]sizeInfo
	check      dest.Check
	editField  field // text field being edited on the summary, if any
	destInput  textinput.Model
	readyNotes []string // shown on the result screen, e.g. "profile saved"

	// Copy in progress.
	copyCh        chan tea.Msg
	copyCancel    context.CancelFunc
	progress      backup.Progress
	bar           progress.Model
	confirmCancel bool
	canceling     bool
	copyResult    backup.Result
	copyErr       error

	// Profiles. usingProfile is the profile chosen with "Use saved profile";
	// editingProfile is the one opened with "e". Both are empty otherwise.
	usingProfile   string
	editingProfile string
	saveProf       bool   // save the custom selection as a profile
	saveAsNew      bool   // when editing: save as a new profile instead of updating
	profileName    string // name the profile will be saved with
	nameEdited     bool   // the user typed the name; don't replace it with a proposal
	nameInput      textinput.Model
	confirmDelete  string // profile waiting for delete confirmation
}

// field is a text field on the summary screen.
type field int

const (
	fieldNone field = iota
	fieldDest
	fieldName
)

type sizeInfo struct {
	bytes int64
	done  bool
	err   error
}

type (
	scanDoneMsg device.Result
	listMsg     struct {
		src     source.Source
		rel     string
		entries []source.Entry
		err     error
	}
	sizeMsg struct {
		src   source.Source
		rel   string
		bytes int64
		err   error
	}
)

func New(opts Options) Model {
	sp := spinner.New()
	sp.Spinner = spinner.Dot
	sp.Style = accent
	ti := textinput.New()
	ti.Prompt = "› "
	ti.CharLimit = 4096
	ni := textinput.New()
	ni.Prompt = "› "
	ni.CharLimit = profile.MaxNameLen
	bar := progress.New(progress.WithGradient(string(accentColor.Dark), string(accentColor.Light)))
	return Model{opts: opts, spinner: sp, destInput: ti, nameInput: ni, bar: bar}
}

func scan() tea.Msg {
	ctx, cancel := context.WithTimeout(context.Background(), scanTimeout)
	defer cancel()
	return scanDoneMsg(device.Scan(ctx))
}

// startSession prepares the backup of a device.
func (m *Model) startSession(d device.Device) {
	m.endSession()
	m.ctx, m.cancel = context.WithCancel(context.Background())
	m.dev = d
	m.src = source.For(d)
	m.sizes = map[string]sizeInfo{}
	m.tree = nil
	m.modeCursor = 0
}

// endSession cancels everything still running for the previous device.
func (m *Model) endSession() {
	if m.cancel != nil {
		m.cancel()
	}
	m.ctx, m.cancel = nil, nil
	m.dev, m.src, m.tree = device.Device{}, nil, nil
	m.usingProfile, m.editingProfile, m.confirmDelete = "", "", ""
	m.saveAsNew, m.nameEdited = false, false
}

func listCmd(parent context.Context, src source.Source, rel string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, listTimeout)
		defer cancel()
		es, err := src.List(ctx, rel)
		return listMsg{src: src, rel: rel, entries: es, err: err}
	}
}

func sizeCmd(parent context.Context, src source.Source, rel string) tea.Cmd {
	return func() tea.Msg {
		select {
		case sizeSlots <- struct{}{}:
			defer func() { <-sizeSlots }()
		case <-parent.Done():
			return sizeMsg{src: src, rel: rel, err: parent.Err()}
		}
		ctx, cancel := context.WithTimeout(parent, sizeTimeout)
		defer cancel()
		n, err := src.Size(ctx, rel)
		return sizeMsg{src: src, rel: rel, bytes: n, err: err}
	}
}

// requestSizes requests, in the background, the sizes not known yet.
func (m *Model) requestSizes(rels ...string) tea.Cmd {
	var cmds []tea.Cmd
	for _, rel := range rels {
		if _, ok := m.sizes[rel]; ok {
			continue
		}
		m.sizes[rel] = sizeInfo{}
		cmds = append(cmds, sizeCmd(m.ctx, m.src, rel))
	}
	return tea.Batch(cmds...)
}

// totalSize adds up the size of a selection. ok is false while some size is
// still being computed.
func (m Model) totalSize(includes []string) (total int64, ok bool) {
	ok = true
	for _, rel := range includes {
		s := m.sizes[rel]
		if !s.done {
			ok = false
		}
		total += s.bytes
	}
	return total, ok
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.spinner.Tick, scan)
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		if m.tree != nil {
			m.scrollTree() // wrapped rows change height with the width
		}
		return m, nil

	case scanDoneMsg:
		m.result = device.Result(msg)
		m.screen = screenDevices
		m.devCursor = min(m.devCursor, max(len(m.result.Devices)-1, 0))
		return m, nil

	case listMsg:
		if msg.src != m.src || m.tree == nil {
			return m, nil // reply for a previous device
		}
		m.tree.setChildren(msg.rel, msg.entries, msg.err)
		var dirs []string
		for _, e := range msg.entries {
			rel := source.Join(msg.rel, e.Name)
			if !e.IsDir {
				m.sizes[rel] = sizeInfo{bytes: e.Size, done: true}
			} else if !e.Restricted {
				dirs = append(dirs, rel)
			}
		}
		return m, m.requestSizes(dirs...)

	case sizeMsg:
		if msg.src != m.src {
			return m, nil
		}
		m.sizes[msg.rel] = sizeInfo{bytes: msg.bytes, done: true, err: msg.err}
		return m, nil

	case copyProgressMsg:
		m.progress = backup.Progress(msg)
		return m, waitCopy(m.copyCh)

	case copyDoneMsg:
		m.copyResult, m.copyErr = msg.res, msg.err
		m.progress = msg.res.Progress
		m.copyCancel()
		m.screen = screenDone
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case tea.KeyMsg:
		// During a copy ctrl+c asks first, like esc, so files are never left
		// half-written.
		if msg.String() == "ctrl+c" && m.screen != screenCopy {
			return m, tea.Quit
		}
		if m.editField != fieldNone {
			return m.updateField(msg)
		}
		m.notice = ""
		switch m.screen {
		case screenDevices:
			return m.updateDevices(msg.String())
		case screenHelp:
			return m.updateHelp(msg.String())
		case screenMode:
			return m.updateMode(msg.String())
		case screenTree:
			return m.updateTree(msg.String())
		case screenSummary:
			return m.updateSummary(msg.String())
		case screenCopy:
			return m.updateCopy(msg.String())
		case screenDone:
			return m.updateDone(msg.String())
		}
	}
	return m, nil
}

func (m Model) View() string {
	var header, body string
	switch m.screen {
	case screenScanning:
		header, body = m.banner(), m.spinner.View()+" Looking for devices..."
	case screenDevices:
		header, body = m.banner(), m.viewDevices()
	case screenHelp:
		w := m.contentWidth()
		header, body = m.compactHeader(), wrap(helpText, w)+"\n"+footer(w, "esc back", "q quit")
	case screenMode:
		header, body = m.banner(), m.viewMode()
	case screenTree:
		header, body = m.compactHeader(), m.viewTree()
	case screenSummary:
		header, body = m.compactHeader(), m.viewSummary()
	case screenCopy:
		header, body = m.compactHeader(), m.viewCopy()
	case screenDone:
		header, body = m.compactHeader(), m.viewDone()
	}

	// Everything is anchored to the top left: the layout never moves with the
	// terminal size, only long text wraps.
	return m.box().Render(lipgloss.JoinVertical(lipgloss.Left, header, "", body)) + "\n"
}

func (m Model) banner() string {
	return banner(m.opts.Version, m.width)
}

// compactHeader is used on screens that need more vertical space.
func (m Model) compactHeader() string {
	h := title.Render(appName) + dim.Render(" v"+m.opts.Version)
	if m.dev.Name == "" || m.screen == screenHelp {
		return h
	}
	// The device name wraps in its own column, after the app name.
	h += dim.Render("  ·  ")
	return row(m.contentWidth(), col{w: lipgloss.Width(h), s: h}, col{s: kindBadgePlain(m.dev.Kind) + " " + selectedStyle.Render(m.dev.Name)})
}

// box frames the content; narrow windows get less padding.
func (m Model) box() lipgloss.Style {
	if m.width > 0 && m.width-box.GetHorizontalFrameSize() < narrowWidth {
		return boxNarrow
	}
	return box
}

// contentWidth is the width available for the screen's content.
func (m Model) contentWidth() int {
	w := 80
	if m.width > 0 {
		w = min(w, m.width-m.box().GetHorizontalFrameSize())
	}
	return max(w, 20)
}

// wrap wraps a paragraph into lines of width w.
func wrap(s string, w int) string {
	return lipgloss.NewStyle().Width(w).Render(s)
}

// col is one column of a row. A zero width takes the space left over.
type col struct {
	w     int
	s     string
	right bool // align to the right
}

// row lays out columns side by side within width total. Text that doesn't fit
// wraps onto the next line inside its own column instead of being cut.
func row(total int, cols ...col) string {
	used, flex := 0, -1
	for i, c := range cols {
		if c.w == 0 {
			flex = i
		} else {
			used += c.w
		}
	}
	parts := make([]string, len(cols))
	for i, c := range cols {
		w := c.w
		if i == flex {
			// Never wider than what is left, so the row can't overflow.
			w = max(total-used, 1)
		}
		st := lipgloss.NewStyle().Width(w)
		if c.right {
			st = st.Align(lipgloss.Right)
		}
		parts[i] = st.Render(c.s)
	}
	return lipgloss.JoinHorizontal(lipgloss.Top, parts...)
}

// minFlexWidth is the smallest useful width for a text input.
const minFlexWidth = 8

// footer lays out key hints over as many lines as needed, always breaking
// between hints and never in the middle of one.
func footer(w int, keys ...string) string {
	var lines []string
	line := ""
	for _, k := range keys {
		switch {
		case line == "":
			line = k
		case lipgloss.Width(line+" · "+k) <= w:
			line += " · " + k
		default:
			lines = append(lines, line)
			line = k
		}
	}
	if line != "" {
		lines = append(lines, line)
	}
	return dim.Render(wrap(strings.Join(lines, "\n"), w))
}
