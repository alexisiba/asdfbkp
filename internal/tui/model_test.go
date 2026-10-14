package tui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/alexisiba/asdfbkp/internal/device"
)

func TestMTPAlwaysRecommendsDebugging(t *testing.T) {
	m, _ := New(Options{Version: "0.0.1"}).Update(scanDoneMsg(device.Result{Devices: []device.Device{
		{Kind: device.KindMTP, Name: "Xiaomi Redmi", Path: "/run/user/1000/gvfs/mtp:host=Xiaomi"},
		{Kind: device.KindUSB, Name: "FOTOS", Path: "/media/u/FOTOS", Size: 8 << 30},
	}}))
	view := m.View()
	t.Log("\n" + view)
	if !strings.Contains(view, "USB debugging") || !strings.Contains(view, "wireless debugging") {
		t.Error("device list with an MTP phone must recommend USB/wireless debugging")
	}

	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if view := m.View(); !strings.Contains(view, "wireless debugging") {
		t.Errorf("selected MTP screen must recommend debugging:\n%s", view)
	}
}
