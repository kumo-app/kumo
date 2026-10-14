package main

import (
	"strconv"
	"strings"

	"github.com/egoist/mygo"
)

// entry is one item of Kumo's quick menus (tray and Dock), kept apart from
// mygo's menus so tests can read it.
type entry struct {
	label    string
	kind     entryKind
	checked  bool
	disabled bool
	do       func() // run on the UI thread
	sub      []entry
}

type entryKind int

const (
	plain entryKind = iota
	check
	radio
	line
)

var separator = entry{kind: line}

// menuGroups are the groups whose node the menus switch: the selectors of
// the profile, and GLOBAL in global mode.
func (k *Kumo) menuGroups() []string {
	var out []string
	for _, n := range k.groups {
		g := k.proxies[n]
		if g.Type != "Selector" || (n == "GLOBAL" && k.cfg.Mode != "global") {
			continue
		}
		out = append(out, n)
	}
	return out
}

// quickMenu is the menu of the tray and of the Dock icon. The Dock adds its
// own Quit, so dock leaves Open and Quit out.
func (k *Kumo) quickMenu(dock bool) []entry {
	status := tr("Disconnected")
	switch {
	case !k.online:
		status = tr("Core offline")
	case k.proxyOn:
		status = tr("Connected") + " · " + k.activeNode()
	}
	off := !k.online
	items := []entry{
		{label: "Kumo — " + status, disabled: true},
		separator,
		{label: tr("System proxy"), kind: check, checked: k.proxyOn, disabled: off || k.proxyBusy,
			do: func() { k.setSystemProxy(!k.proxyOn) }},
		{label: tr("Enhanced Mode (TUN)"), kind: check, checked: k.tunOn(), disabled: off || k.tunBusy,
			do: func() { k.setTun(!k.tunOn()) }},
		separator,
	}
	for _, id := range modeIDs {
		items = append(items, entry{label: modeLabel(id), kind: radio, checked: k.cfg.Mode == id, disabled: off,
			do: func() { k.setMode(id) }})
	}
	if groups := k.menuGroups(); len(groups) > 0 && k.cfg.Mode != "direct" {
		items = append(items, separator)
		for _, name := range groups {
			items = append(items, k.groupEntry(name, off))
		}
	}
	if !dock {
		items = append(items, separator,
			entry{label: tr("Open Kumo"), do: k.show},
			entry{label: tr("Quit Kumo"), do: mygo.App.Quit})
	}
	return items
}

// groupEntry is a group's submenu: use the fastest, then its nodes.
func (k *Kumo) groupEntry(name string, off bool) entry {
	g := k.proxies[name]
	sub := []entry{{label: tr("Use fastest"), disabled: off, do: func() { k.pickFastest(name) }}, separator}
	for _, n := range g.All {
		text := n
		if p, ok := k.proxies[n]; ok && !p.IsGroup() && len(p.History) > 0 {
			text += "    " + latencyText(p.LastDelay(), true)
		}
		sub = append(sub, entry{label: text, kind: radio, checked: g.Now == n, disabled: off,
			do: func() { k.choose(name, n) }})
	}
	return entry{label: name + " — " + k.resolve(name), sub: sub}
}

// native turns entries into mygo menu items; clicks run on the UI thread.
func (k *Kumo) native(items []entry) []*mygo.MenuItem {
	out := make([]*mygo.MenuItem, 0, len(items))
	for _, e := range items {
		if e.kind == line {
			out = append(out, mygo.Separator())
			continue
		}
		it := &mygo.MenuItem{Label: e.label, Checked: e.checked, Disabled: e.disabled}
		switch e.kind {
		case check:
			it.Type = mygo.MenuItemCheckbox
		case radio:
			it.Type = mygo.MenuItemRadio
		}
		if e.sub != nil {
			it.Submenu = k.native(e.sub)
		}
		if do := e.do; do != nil {
			it.Click = func(*mygo.MenuItem, *mygo.Window) { k.post(do) }
		}
		out = append(out, it)
	}
	return out
}

// refreshMenus rebuilds the tray and Dock menus so they follow the state.
func (k *Kumo) refreshMenus() {
	if k.tray != nil {
		k.tray.SetMenu(mygo.NewMenu(k.native(k.quickMenu(false))))
	}
	if k.dockMenu {
		mygo.App.Dock.SetMenu(mygo.NewMenu(k.native(k.quickMenu(true))))
	}
}

// menuSignature sums up what the menus show, so they are rebuilt only when
// it changes.
func menuSignature(items []entry) string {
	var b strings.Builder
	var walk func([]entry)
	walk = func(es []entry) {
		for _, e := range es {
			b.WriteString(e.label)
			b.WriteString(strconv.FormatBool(e.checked))
			b.WriteString(strconv.FormatBool(e.disabled))
			if e.sub != nil {
				b.WriteByte('[')
				walk(e.sub)
				b.WriteByte(']')
			}
			b.WriteByte('|')
		}
	}
	walk(items)
	return b.String()
}

// syncMenus rebuilds the menus when what they show has changed.
func (k *Kumo) syncMenus() {
	k.syncSpeed()
	if k.tray == nil && !k.dockMenu {
		return
	}
	if sig := menuSignature(k.quickMenu(false)); sig != k.menuSig {
		k.menuSig = sig
		k.refreshMenus()
	}
}
