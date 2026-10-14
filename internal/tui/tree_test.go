package tui

import (
	"reflect"
	"testing"

	"github.com/alexisiba/asdfbkp/internal/source"
)

func dirs(names ...string) []source.Entry {
	var es []source.Entry
	for _, n := range names {
		es = append(es, source.Entry{Name: n, IsDir: true})
	}
	return es
}

func TestTreeToggleAndIncludes(t *testing.T) {
	tr := newTree(nil)
	tr.setChildren(".", dirs("DCIM", "Documents", "Download"), nil)
	tr.setChildren("DCIM", dirs("Camera", "Screenshots"), nil)

	tr.toggle(tr.nodes["DCIM"])
	if got := tr.includes(); !reflect.DeepEqual(got, []string{"DCIM"}) {
		t.Fatalf("includes = %v", got)
	}

	tr.toggle(tr.nodes["DCIM/Screenshots"])
	if tr.nodes["DCIM"].state != checkPartial || tr.root.state != checkPartial {
		t.Errorf("parent should be partial after unchecking a child")
	}
	tr.toggle(tr.nodes["Documents"])
	if got := tr.includes(); !reflect.DeepEqual(got, []string{"DCIM/Camera", "Documents"}) {
		t.Errorf("includes = %v", got)
	}

	// Marking everything collapses the selection into ".".
	tr.toggle(tr.root)
	if got := tr.includes(); !reflect.DeepEqual(got, []string{"."}) {
		t.Errorf("includes after select all = %v", got)
	}
}

func TestTreeChildrenInheritState(t *testing.T) {
	tr := newTree(nil)
	tr.setChildren(".", dirs("DCIM"), nil)
	tr.toggle(tr.nodes["DCIM"])
	tr.setChildren("DCIM", dirs("Camera"), nil)
	if tr.nodes["DCIM/Camera"].state != checkAll {
		t.Error("children loaded under a checked folder must be checked")
	}
}

func TestTreeFromProfile(t *testing.T) {
	tr := newTree([]string{"DCIM/Camera", "Documents"})
	tr.setChildren(".", dirs("DCIM", "Documents", "Music"), nil)
	if s := tr.nodes["DCIM"].state; s != checkPartial {
		t.Errorf("DCIM state = %v, want partial", s)
	}
	if s := tr.nodes["Documents"].state; s != checkAll {
		t.Errorf("Documents state = %v, want all", s)
	}
	// Without opening DCIM, the profile's selection is kept.
	if got := tr.includes(); !reflect.DeepEqual(got, []string{"DCIM/Camera", "Documents"}) {
		t.Errorf("includes = %v", got)
	}
	tr.setChildren("DCIM", dirs("Camera", "Screenshots"), nil)
	if tr.nodes["DCIM/Camera"].state != checkAll || tr.nodes["DCIM/Screenshots"].state != checkNone {
		t.Error("profile selection must be applied when a partial folder is opened")
	}
}

func TestTreeRestricted(t *testing.T) {
	tr := newTree(nil)
	tr.setChildren(".", []source.Entry{
		{Name: "data", IsDir: true, Restricted: true},
		{Name: "media", IsDir: true},
	}, nil)
	tr.toggle(tr.nodes["data"])
	if tr.nodes["data"].state != checkNone {
		t.Error("restricted folders cannot be selected")
	}
	tr.toggle(tr.nodes["media"])
	if tr.root.state != checkAll {
		t.Error("restricted folders must not keep the parent partial")
	}
}

func TestTreeHiddenFilesKeepParentPartial(t *testing.T) {
	tr := newTree(nil)
	tr.setChildren(".", []source.Entry{{Name: "DCIM", IsDir: true}, {Name: "note.txt", Size: 3}}, nil)
	tr.toggle(tr.nodes["DCIM"])
	if got := tr.includes(); !reflect.DeepEqual(got, []string{"DCIM"}) {
		t.Errorf("unselected files must stay out of the backup: %v", got)
	}
	if len(tr.visible()) != 1 {
		t.Error("files are hidden by default")
	}
	tr.showFiles = true
	if len(tr.visible()) != 2 {
		t.Error("files are shown when toggled")
	}
}
