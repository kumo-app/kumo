package main

import (
	"runtime"

	"github.com/egoist/mygo"
	"strings"

	"github.com/egoist/mygo/ui"
)

// keyNames are the accelerator names of the keys a shortcut can end with.
var keyNames = func() map[ui.Key]string {
	m := map[ui.Key]string{
		ui.KeySpace: "Space", ui.KeyEnter: "Enter", ui.KeyTab: "Tab", ui.KeyDelete: "Delete", ui.KeyInsert: "Insert",
		ui.KeyHome: "Home", ui.KeyEnd: "End", ui.KeyPageUp: "PageUp", ui.KeyPageDown: "PageDown",
		ui.KeyLeft: "Left", ui.KeyRight: "Right", ui.KeyUp: "Up", ui.KeyDown: "Down",
		ui.KeyMinus: "-", ui.KeyEqual: "=", ui.KeyComma: ",", ui.KeyPeriod: ".", ui.KeySlash: "/",
		ui.KeySemicolon: ";", ui.KeyQuote: "'", ui.KeyBracketLeft: "[", ui.KeyBracketRight: "]",
		ui.KeyBackslash: "\\", ui.KeyBackquote: "`",
	}
	for i, k := range []ui.Key{ui.KeyF1, ui.KeyF2, ui.KeyF3, ui.KeyF4, ui.KeyF5, ui.KeyF6, ui.KeyF7, ui.KeyF8, ui.KeyF9, ui.KeyF10, ui.KeyF11, ui.KeyF12} {
		m[k] = "F" + itoa(i+1)
	}
	for i, k := range []ui.Key{ui.Key0, ui.Key1, ui.Key2, ui.Key3, ui.Key4, ui.Key5, ui.Key6, ui.Key7, ui.Key8, ui.Key9} {
		m[k] = itoa(i)
	}
	letters := []ui.Key{ui.KeyA, ui.KeyB, ui.KeyC, ui.KeyD, ui.KeyE, ui.KeyF, ui.KeyG, ui.KeyH, ui.KeyI, ui.KeyJ, ui.KeyK, ui.KeyL, ui.KeyM,
		ui.KeyN, ui.KeyO, ui.KeyP, ui.KeyQ, ui.KeyR, ui.KeyS, ui.KeyT, ui.KeyU, ui.KeyV, ui.KeyW, ui.KeyX, ui.KeyY, ui.KeyZ}
	for i, k := range letters {
		m[k] = string(rune('A' + i))
	}
	return m
}()

// accelerator turns a recorded key press into a shortcut: the modifiers in
// mygo's order, then the key. A global shortcut needs a modifier other
// than Shift, except on the function keys; ok is false otherwise.
func accelerator(mods ui.Modifiers, key ui.Key) (string, bool) {
	name, known := keyNames[key]
	if !known {
		return "", false
	}
	var parts []string
	for _, m := range []struct {
		mod  ui.Modifiers
		name string
	}{{ui.Super, "Super"}, {ui.Ctrl, "Ctrl"}, {ui.Alt, "Alt"}, {ui.Shift, "Shift"}} {
		if mods&m.mod != 0 {
			parts = append(parts, m.name)
		}
	}
	fkey := len(name) > 1 && name[0] == 'F'
	if mods&^ui.Shift == 0 && !fkey {
		return "", false
	}
	return strings.Join(append(parts, name), "+"), true
}

// hotkeyLabel shows a shortcut the platform's way: ⌃⌥⇧⌘K on macOS,
// Ctrl+Alt+K elsewhere.
func hotkeyLabel(acc, goos string) string {
	if acc == "" {
		return ""
	}
	parts := strings.Split(acc, "+")
	if strings.HasSuffix(acc, "++") {
		parts = append(parts[:len(parts)-2], "+")
	}
	key := parts[len(parts)-1]
	mods := map[string]bool{}
	for _, p := range parts[:len(parts)-1] {
		mods[p] = true
	}
	if goos == "darwin" {
		out := ""
		for _, m := range []struct{ name, sym string }{{"Ctrl", "⌃"}, {"Alt", "⌥"}, {"Shift", "⇧"}, {"Super", "⌘"}} {
			if mods[m.name] {
				out += m.sym
			}
		}
		return out + key
	}
	var out []string
	for _, m := range []struct{ name, label string }{{"Ctrl", "Ctrl"}, {"Alt", "Alt"}, {"Shift", "Shift"}, {"Super", "Win"}} {
		if mods[m.name] {
			out = append(out, m.label)
		}
	}
	if goos == "linux" {
		for i, m := range out {
			if m == "Win" {
				out[i] = "Super"
			}
		}
	}
	return strings.Join(append(out, key), "+")
}

// hotkeyField shows an action's shortcut and records a new one: click it,
// press the keys; Escape cancels, Backspace or Delete clears.
func (k *Kumo) hotkeyField(c *ui.Context, s skin, action string) {
	recording := k.hotkeyRec == action
	text := hotkeyLabel(k.prefs.App.Hotkeys[action], runtime.GOOS)
	color := s.text
	switch {
	case recording:
		text, color = tr("Press keys…"), s.accent
	case text == "":
		text, color = tr("None"), s.muted
	}
	b := ui.ButtonBase(c.Key("hk-"+action)).Width(170).Height(30).Radius(7).Padding(0, 10).Gap(6).AlignItems(ui.Center).
		BorderWidth(1, 1, 1, 1).BorderColor(s.line).Label(tr("Shortcut")).Focusable()
	if recording {
		b.BorderColor(s.accent).Background(s.accent.Alpha(0.08))
	} else if b.Hovered() {
		b.Background(s.hover)
	}
	if k.hotkeyErr[action] != "" && !recording {
		b.BorderColor(slow).Tooltip(tr("Another app may use this shortcut: ") + k.hotkeyErr[action])
	}
	b.Children(func() {
		ui.Icon(c, glyph("keyboard")).FontSize(14).TextColor(s.muted)
		ui.Text(c, text).FontSize(13).FontFeatures("tnum").TextColor(color).SingleLine().Grow(1).MinWidth(0)
	})
	if b.Clicked() {
		if recording {
			k.recordHotkey("")
		} else {
			k.recordHotkey(action)
		}
	}
	if recording {
		b.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputKeyDown {
				return false
			}
			switch {
			case ev.Key == ui.KeyEscape && ev.Mods == 0:
				k.recordHotkey("")
			case (ev.Key == ui.KeyBackspace || ev.Key == ui.KeyDelete) && ev.Mods == 0:
				k.hotkeyRec = ""
				k.setHotkey(action, "")
			default:
				acc, ok := accelerator(ev.Mods, ev.Key)
				if !ok {
					return true // a modifier alone, or no modifier yet
				}
				k.hotkeyRec = ""
				k.setHotkey(action, acc)
			}
			return true
		})
		if !b.Focused() {
			b.Focus()
		}
	}
	if k.prefs.App.Hotkeys[action] != "" && !recording && roundButton(c.Key("hkx-"+action), s, "x", tr("Clear")).Clicked() {
		k.setHotkey(action, "")
	}
}

// recordHotkey starts recording action's shortcut, or stops with "". While
// recording, Kumo's global shortcuts are off, so that pressing one records
// it (and shows the conflict) instead of running it.
func (k *Kumo) recordHotkey(action string) {
	was := k.hotkeyRec
	k.hotkeyRec = action
	switch {
	case action != "" && was == "":
		mygo.GlobalShortcut.UnregisterAll()
	case action == "" && was != "":
		k.registerHotkeys()
	}
}
