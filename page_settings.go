package main

import (
	"fmt"
	"os"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/transfer"
	"github.com/egoist/mygo/ui"

	"kumo/clash"
	"kumo/core"
)

// version is the app's version, as in mygo.json.
var version = "0.4.4"

// section heads a group of settings.
func section(c *ui.Context, s skin, title string) {
	ui.Text(c, title).FontSize(12).FontWeight(600).LetterSpacing(0.4).TextColor(s.muted).Padding(22, 0, 4, 0).SingleLine()
}

// setting is one setting: its name over what it does, and its control.
func setting(c *ui.Context, s skin, title, detail string, control func()) {
	ui.Row(c.Key(title)).Padding(12, 0).Gap(24).AlignItems(ui.Center).BorderWidth(0, 0, 1, 0).BorderColor(s.line).Children(func() {
		ui.Column(c).Grow(1).Basis(0).Gap(3).Children(func() {
			ui.Text(c, title).FontSize(14).FontWeight(500)
			if detail != "" {
				ui.Text(c, detail).FontSize(12.5).TextColor(s.muted).LineHeight(1.4)
			}
		})
		control()
	})
}

// settingsColumn is the scrolling, width-capped body of a settings page.
func settingsColumn(c *ui.Context, fn func()) {
	ui.Scroll(c).Grow(1).Padding(0, 24, 24, 24).Children(func() {
		ui.Column(c).MaxWidth(760).Children(fn)
	})
}

func (k *Kumo) settingsPage(c *ui.Context, s skin) {
	k.header(c, s, tr("Settings"), nil)
	settingsColumn(c, func() {
		section(c, s, tr("GENERAL"))
		setting(c, s, tr("Turn on the system proxy at launch"), tr("Off by default: Kumo only changes your network when you press Connect."), func() {
			if ui.Switch(c, &k.prefs.Core.AutoSystemProxy).Label(tr("Turn on the system proxy at launch")).Changed() {
				_ = k.prefs.save()
			}
		})
		setting(c, s, tr("Advanced mode"), tr("Adds the core settings (ports, TUN, LAN, external controller) and the running configuration to the sidebar."), func() {
			if ui.Switch(c, &k.prefs.UI.AdvancedMode).Label(tr("Advanced mode")).Changed() {
				_ = k.prefs.save()
			}
		})
		setting(c, s, tr("Show speed in menu bar"), tr("Upload and download speed beside the Kumo icon; in the tooltip on Windows."), func() {
			show := !k.prefs.App.HideTraySpeed
			if ui.Switch(c, &show).Label(tr("Show speed in menu bar")).Changed() {
				k.prefs.App.HideTraySpeed = !show
				_ = k.prefs.save()
			}
		})
		setting(c, s, tr("Theme"), "", func() {
			ids := []string{"system", "light", "dark"}
			i := max(slices.Index(ids, k.prefs.App.Theme), 0)
			if ui.Segmented(c, &i, tr("System"), tr("Light"), tr("Dark")).Label(tr("Theme")).Changed() {
				k.prefs.App.Theme = ids[i]
				applyTheme(k.prefs)
				_ = k.prefs.save()
			}
		})
		setting(c, s, tr("Sidebar color"), tr("Tints the sidebar and the window."), func() {
			ui.Row(c).Gap(10).Children(func() {
				for _, h := range sidebarHues {
					b := ui.ButtonBase(c.Key(h.id)).Size(24, 24).Radius(12).Label(h.id).Background(swatchOf(h.hue, h.gray))
					if k.prefs.App.SidebarColor == h.id {
						b.Border(2, s.text)
					}
					if b.Clicked() {
						k.prefs.App.SidebarColor = h.id
						_ = k.prefs.save()
					}
				}
			})
		})
		setting(c, s, tr("Language"), "", func() {
			ids := languages
			names := []string{"繁體中文", "English"}
			cur := names[max(slices.Index(ids, k.prefs.App.Language), 0)]
			if ui.Select(c, &cur, names).Label(tr("Language")).Width(200).Changed() {
				k.prefs.App.Language = ids[max(slices.Index(names, cur), 0)]
				_ = k.prefs.save()
				k.refreshMenus()
			}
		})
		section(c, s, tr("ABOUT"))
		ui.Text(c, fmt.Sprintf(tr("Kumo %s is a simple desktop client for Clash / mihomo, built with MyGo and MIT licensed. It bundles the mihomo core (GPL-3.0)."), version)).
			FontSize(13).TextColor(s.muted).LineHeight(1.45).Padding(10, 0)
	})
}

// applyTheme follows the theme preference.
func applyTheme(p *Prefs) {
	switch p.App.Theme {
	case "light":
		mygo.Theme.SetSource(mygo.ThemeLight)
	case "dark":
		mygo.Theme.SetSource(mygo.ThemeDark)
	default:
		mygo.Theme.SetSource(mygo.ThemeSystem)
	}
}

// Core (advanced mode).

func (k *Kumo) corePage(c *ui.Context, s skin) {
	k.header(c, s, tr("Core"), nil)
	settingsColumn(c, func() {
		section(c, s, tr("RUNNING CORE"))
		if k.online {
			k.runningCore(c, s)
		} else {
			ui.Text(c, tr("The core is not connected; its settings show once it answers.")).FontSize(13).TextColor(s.muted).Padding(12, 0)
		}
		section(c, s, tr("CORE SOURCE"))
		k.coreSource(c, s)
		section(c, s, tr("LATENCY TEST"))
		setting(c, s, tr("Test URL"), tr("Nodes are timed by fetching this address through them."), func() {
			if ui.TextInput(c, &k.prefs.Controller.TestURL).Label(tr("Test URL")).Width(320).Changed() {
				_ = k.prefs.save()
			}
		})
	})
}

func (k *Kumo) runningCore(c *ui.Context, s skin) {
	cfg := &k.cfg
	setting(c, s, tr("Mode"), tr("Rule follows the rules, Global sends everything through GLOBAL, Direct sends nothing through a proxy."), func() {
		i := max(slices.Index(modeIDs, cfg.Mode), 0)
		if ui.Segmented(c, &i, tr("Rule"), tr("Global"), tr("Direct")).Label(tr("Mode")).Changed() {
			k.setMode(modeIDs[i])
		}
	})
	port := func(title, key string, v *int) {
		setting(c, s, title, tr("0 turns it off."), func() {
			f := float64(*v)
			if ui.NumberInput(c, &f, 0, 65535, 1).Label(title).Width(130).Changed() {
				*v = int(f)
				k.patch(map[string]any{key: *v})
			}
		})
	}
	port(tr("Mixed port"), "mixed-port", &cfg.MixedPort)
	port(tr("HTTP port"), "port", &cfg.Port)
	port(tr("SOCKS port"), "socks-port", &cfg.SocksPort)
	setting(c, s, tr("Allow LAN"), tr("Accept connections from other devices on the network."), func() {
		if ui.Switch(c, &cfg.AllowLan).Label(tr("Allow LAN")).Changed() {
			k.setAllowLan(cfg.AllowLan)
		}
	})
	setting(c, s, tr("Enhanced Mode (TUN)"), tr("Asks once for an administrator's authorization: macOS installs a root-owned copy of the core, Linux grants it network rights with pkexec and setcap, Windows needs Kumo run as administrator."), func() {
		if k.tunBusy {
			ui.Spinner(c).Size(16, 16)
		}
		on := k.tunOn()
		if ui.Switch(c, &on).Label(tr("Enhanced Mode (TUN)")).Changed() {
			k.setTun(on)
		}
	})
	setting(c, s, "IPv6", "", func() {
		if ui.Switch(c, &cfg.IPv6).Label("IPv6").Changed() {
			k.patch(map[string]any{"ipv6": cfg.IPv6})
		}
	})
	setting(c, s, tr("Log level"), "", func() {
		lv := cfg.LogLevel
		if ui.Select(c, &lv, logLevels).Label(tr("Log level")).Width(160).Changed() {
			cfg.LogLevel = lv
			k.patch(map[string]any{"log-level": lv})
		}
	})
}

func (k *Kumo) coreSource(c *ui.Context, s skin) {
	if k.mgr == nil {
		k.controllerForm(c, s)
		return
	}
	setting(c, s, tr("Core"), tr("Run the mihomo core bundled with Kumo, or connect to a Clash / mihomo controller that runs elsewhere."), func() {
		i := 0
		if k.prefs.Core.External {
			i = 1
		}
		if ui.Segmented(c, &i, tr("Bundled"), tr("External")).Label(tr("Core")).Changed() {
			k.prefs.Core.External = i == 1
			_ = k.prefs.save()
			k.startCore()
		}
	})
	if k.prefs.Core.External {
		k.controllerForm(c, s)
		return
	}
	info := func(title, value string) {
		setting(c, s, title, "", func() {
			ui.Text(c, value).FontSize(12.5).TextColor(s.muted).SingleLine().MaxWidth(380).Selectable()
		})
	}
	state := tr("Running")
	switch {
	case k.coreState == core.Failed:
		state = tr("Failed") + ": " + k.coreErr
	case !k.online:
		state = tr("Starting")
	}
	info(tr("Status"), state)
	info(tr("Core binary"), k.mgr.Binary)
	info(tr("Version"), k.coreVersion)
	info(tr("Home directory"), k.mgr.Dir)
	info(tr("Controller"), "127.0.0.1:"+itoa(k.prefs.Core.Port))
	setting(c, s, tr("Default mixed port"), tr("For profiles that open no port of their own. Applies on restart."), func() {
		v := float64(k.prefs.Core.MixedPort)
		if ui.NumberInput(c, &v, 1, 65535, 1).Label(tr("Default mixed port")).Width(130).Changed() {
			k.prefs.Core.MixedPort = int(v)
			_ = k.prefs.save()
		}
	})
	setting(c, s, tr("Restart core"), tr("Rewrites the runtime configuration from the active profile."), func() {
		if ui.Button(c, tr("Restart")).Clicked() {
			k.confirmRestart()
		}
	})
}

// controllerForm edits the external controller; changes apply on save.
func (k *Kumo) controllerForm(c *ui.Context, s skin) {
	if !k.ctlReady {
		k.ctl = controllerForm{host: k.prefs.Controller.Host, port: itoa(k.prefs.Controller.Port), secret: k.prefs.Controller.Secret, tls: k.prefs.Controller.TLS}
		k.ctlReady = true
	}
	d := &k.ctl
	setting(c, s, tr("Host"), tr("Address of external-controller."), func() {
		ui.TextInput(c, &d.host).Label(tr("Host")).Placeholder("127.0.0.1").Width(260)
	})
	setting(c, s, tr("Port"), "", func() {
		ui.TextInput(c, &d.port).Label(tr("Port")).Width(130)
	})
	setting(c, s, tr("Secret"), tr("The secret of the configuration, sent as a bearer token."), func() {
		ui.TextInput(c, &d.secret).Label(tr("Secret")).Placeholder(tr("none")).Password().Width(260)
	})
	setting(c, s, tr("Use HTTPS"), tr("Encrypts the connection to the controller (https and wss). The controller must have TLS set up."), func() {
		ui.Switch(c, &d.tls).Label(tr("Use HTTPS"))
	})
	if !d.tls && !clash.Loopback(strings.TrimSpace(d.host)) && strings.TrimSpace(d.host) != "" {
		ui.Text(c, tr("This controller is on another machine: without HTTPS its secret and your connections travel unencrypted over the network.")).
			FontSize(13).TextColor(slow).LineHeight(1.4).Padding(4, 0)
	}
	setting(c, s, tr("Latency timeout"), tr("Milliseconds."), func() {
		v := float64(k.prefs.Controller.TestTimeout)
		if ui.NumberInput(c, &v, 500, 30000, 500).Label(tr("Latency timeout")).Width(130).Changed() {
			k.prefs.Controller.TestTimeout = int(v)
			_ = k.prefs.save()
		}
	})
	p := k.prefs.Controller
	if d.host == p.Host && d.port == itoa(p.Port) && d.secret == p.Secret && d.tls == p.TLS {
		return
	}
	ui.Row(c).Padding(12, 0).Gap(8).Justify(ui.End).AlignItems(ui.Center).Children(func() {
		ui.Text(c, tr("Changes apply on save, and reconnect.")).FontSize(13).TextColor(s.muted).Grow(1)
		if ui.Button(c, tr("Discard")).Clicked() {
			k.ctlReady = false
		}
		if ui.PrimaryButton(c, tr("Save")).Clicked() {
			port, err := strconv.Atoi(d.port)
			if err != nil || port <= 0 || port > 65535 {
				k.say(tr("Enter a port between 1 and 65535."))
				return
			}
			k.prefs.Controller.Host, k.prefs.Controller.Port, k.prefs.Controller.Secret, k.prefs.Controller.TLS = d.host, port, d.secret, d.tls
			_ = k.prefs.save()
			k.startCore()
			k.say(tr("Connecting to ") + d.host + ":" + d.port)
		}
	})
}

// Configuration (advanced mode).

func (k *Kumo) configText() (string, string) {
	if !k.bundled() {
		return "", ""
	}
	path := k.runtimeFile()
	data, err := os.ReadFile(path)
	if err != nil {
		return path, err.Error()
	}
	return path, readableYAML(string(data))
}

var yamlEscape = regexp.MustCompile(`\\U[0-9A-Fa-f]{8}|\\u[0-9A-Fa-f]{4}`)

// readableYAML turns the \U and \u escapes the YAML encoder writes for
// emoji back into the characters; inside double quotes both mean the same.
func readableYAML(text string) string {
	return yamlEscape.ReplaceAllStringFunc(text, func(m string) string {
		n, err := strconv.ParseUint(m[2:], 16, 32)
		if err != nil || !utf8.ValidRune(rune(n)) || n < 0x20 || n == '"' || n == '\\' {
			return m
		}
		return string(rune(n))
	})
}

func (k *Kumo) configPage(c *ui.Context, s skin) {
	path, text := k.configText()
	k.header(c, s, tr("Configuration"), func() {
		if path != "" && ui.Button(c, tr("Copy")).Clicked() {
			_ = mygo.Clipboard.Write(transfer.TextData(text))
			k.say(tr("Copied"))
		}
		if path != "" && ui.Button(c, tr("Show file")).Clicked() {
			_ = mygo.Shell.OpenPath(path)
		}
	})
	sheetScroll(c, 16, func() {
		if path == "" {
			ui.Text(c, tr("An external core is in use; its configuration is kept by that core.")).FontSize(13).TextColor(s.muted)
			return
		}
		ui.Text(c, path).FontSize(12).TextColor(s.muted).Selectable().Margin(0, 0, 10, 0)
		ui.Text(c, text).Font("monospace").FontSize(12.5).LineHeight(1.5).Selectable()
	})
}
