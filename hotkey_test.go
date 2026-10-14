package main

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

func TestAccelerator(t *testing.T) {
	for _, c := range []struct {
		mods ui.Modifiers
		key  ui.Key
		want string
		ok   bool
	}{
		{ui.Super | ui.Shift, ui.KeyP, "Super+Shift+P", true},
		{ui.Ctrl | ui.Alt, ui.Key1, "Ctrl+Alt+1", true},
		{0, ui.KeyF9, "F9", true},
		{ui.Shift, ui.KeyA, "", false},
		{0, ui.KeyA, "", false},
		{ui.Ctrl, ui.KeyEscape, "", false},
		{ui.Super | ui.Ctrl | ui.Alt | ui.Shift, ui.KeySpace, "Super+Ctrl+Alt+Shift+Space", true},
	} {
		got, ok := accelerator(c.mods, c.key)
		if got != c.want || ok != c.ok {
			t.Errorf("%v %v: %q %v", c.mods, c.key, got, ok)
		}
	}
}

func TestHotkeyLabel(t *testing.T) {
	if got := hotkeyLabel("Super+Ctrl+Alt+Shift+K", "darwin"); got != "⌃⌥⇧⌘K" {
		t.Error(got)
	}
	if got := hotkeyLabel("Super+Ctrl+P", "windows"); got != "Ctrl+Win+P" {
		t.Error(got)
	}
	if got := hotkeyLabel("Super+P", "linux"); got != "Super+P" {
		t.Error(got)
	}
}

func TestHotkeyConflict(t *testing.T) {
	keys := map[string]string{"proxy": "Ctrl+Alt+P", "mode": "Ctrl+Alt+M"}
	if hotkeyConflict(keys, "tun", "ctrl+alt+p") == "" {
		t.Error("conflict missed")
	}
	if hotkeyConflict(keys, "proxy", "Ctrl+Alt+P") != "" {
		t.Error("an action conflicts with itself")
	}
	if hotkeyConflict(keys, "tun", "Ctrl+Alt+T") != "" || hotkeyConflict(keys, "tun", "") != "" {
		t.Error("false conflict")
	}
}
