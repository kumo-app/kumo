package main

import (
	"context"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"

	"github.com/egoist/mygo"

	"kumo/clash"
	"kumo/tun"
)

// reclaim removes files in the core's directory that the user does not
// own: Kumo 0.4.3 ran the Enhanced Mode core as root there, and the files
// it left (cache.db, geodata, providers) would stop the ordinary core from
// saving or updating them. Deleting needs only the directory's rights; the
// core downloads what it needs again.
func reclaim(dir string) {
	uid := os.Getuid()
	if uid <= 0 {
		return // Windows, or root
	}
	var foreign []string
	filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || path == dir {
			return nil
		}
		if info, err := d.Info(); err == nil && fileOwner(info) != uid {
			foreign = append(foreign, path)
			if d.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	for _, p := range foreign {
		if os.RemoveAll(p) != nil {
			// A root-owned folder keeps its contents; move it out of the way.
			_ = os.Rename(p, p+".root-"+time.Now().Format("20060102150405"))
		}
	}
}

// tunDown has a core in Enhanced Mode take its TUN device, routes and DNS
// down before it is killed (Windows cannot interrupt it).
func (k *Kumo) tunDown() {
	cli := k.tunCtl.Load()
	if cli == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
	defer cancel()
	if cfg, err := cli.Configs(ctx); err != nil || !cfg.Tun.Enable {
		return
	}
	_ = cli.PatchConfigs(ctx, map[string]any{"tun": map[string]any{"enable": false}})
}

// setTunCtl keeps the controller of the bundled core for tunDown.
func (k *Kumo) setTunCtl(cli *clash.Client) {
	if k.bundled() {
		k.tunCtl.Store(cli)
	} else {
		k.tunCtl.Store(nil)
	}
}

// checkLegacyTun offers to remove the privileged core Kumo 0.4.3
// installed, which other programs could misuse.
func (k *Kumo) checkLegacyTun() {
	if !tun.Legacy() {
		return
	}
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{Parent: k.win, Type: mygo.MessageWarning,
			Message: tr("Remove the old Enhanced Mode core?"),
			Detail:  fmt.Sprintf(tr("Kumo 0.4.3 installed a copy of the core at “%s” that any program on this computer can run with elevated rights. Kumo no longer uses it. Removing it asks for an administrator password; turn Enhanced Mode on again afterwards to set up its safer replacement."), tun.LegacyPath()),
			Buttons: []string{tr("Remove"), tr("Later")}, DefaultButton: 0, CancelButton: 1})
		if err != nil || res.Button != 0 {
			return
		}
		err = tun.RemoveLegacy()
		k.post(func() {
			if err != nil {
				k.say(explainTunError(err))
				return
			}
			k.say(tr("The old Enhanced Mode core was removed."))
		})
	}()
}

// reportLoadErrors tells the user when saved settings or subscriptions
// could not be read, and where the unreadable file was kept.
func (k *Kumo) reportLoadErrors() {
	if k.prefs.loadErr != "" {
		k.say(tr("Your settings could not be read and were reset; the old file was kept: ") + k.prefs.loadErr)
	}
	if k.profiles.LoadErr != "" {
		k.say(tr("Your subscriptions could not be read: ") + k.profiles.LoadErr)
	}
}
