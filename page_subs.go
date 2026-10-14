package main

import (
	"fmt"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"kumo/core"
)

func (k *Kumo) subsPage(c *ui.Context, s skin) {
	k.header(c, s, tr("Subscriptions"), func() {
		ui.Spacer(c)
		if ui.Button(c, tr("Import file…")).Clicked() {
			k.importProfile()
		}
	})
	sheetScroll(c, 16, func() {
		ui.Column(c).Gap(10).Children(func() {
			label(c, s, tr("ADD SUBSCRIPTION"))
			ui.Text(c, tr("Paste the Clash subscription link from your provider. Kumo downloads it and switches to it.")).FontSize(12.5).TextColor(s.muted)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c, &k.sub.url).Label(tr("Subscription link")).Placeholder(tr("Paste the subscription link here")).Grow(1).MinWidth(200).OnSubmit(k.addSubscription)
				ui.TextInput(c, &k.sub.name).Label(tr("Name")).Placeholder(tr("Name (optional)")).Width(160)
				if k.sub.every == "" {
					k.sub.every = autoUpdateChoices[3]
				}
				names := make([]string, len(autoUpdateChoices))
				for i, l := range autoUpdateChoices {
					names[i] = tr(l)
				}
				every := tr(k.sub.every)
				if ui.Select(c, &every, names).Label(tr("Auto update")).Width(160).Changed() {
					for i, n := range names {
						if n == every {
							k.sub.every = autoUpdateChoices[i]
						}
					}
				}
				if k.sub.adding {
					ui.Spinner(c).Size(18, 18)
				} else if ui.PrimaryButton(c, tr("Import")).Clicked() {
					k.addSubscription()
				}
			})
			if !k.bundled() {
				ui.Text(c, tr("Kumo is connected to an external controller: profiles apply to the bundled core only.")).FontSize(12.5).TextColor(s.muted)
			}
		})
		ui.Column(c).Gap(10).Margin(20, 0, 0, 0).Children(func() {
			label(c, s, tr("YOUR SUBSCRIPTIONS"))
			k.profileRow(c.Key("builtin"), s, core.Profile{Name: tr("Default (direct)")}, !k.hasActive())
			active := k.profiles.Active()
			for _, p := range k.profiles.List() {
				k.profileRow(c.Key(p.ID), s, p, p.ID == active)
			}
		})
	})
}

func (k *Kumo) hasActive() bool {
	_, ok := k.profiles.Find(k.profiles.Active())
	return ok
}

func (k *Kumo) profileRow(c *ui.Context, s skin, p core.Profile, active bool) {
	row := ui.Row(c).Padding(12, 16).Gap(12).Radius(12).AlignItems(ui.Center).BorderWidth(1, 1, 1, 1).BorderColor(s.line)
	if active {
		row.Background(s.accent.Alpha(0.12)).BorderColor(s.accent)
	}
	row.Children(func() {
		icon := "file-down"
		if p.Remote() {
			icon = "link"
		}
		ui.Icon(c, glyph(icon)).FontSize(18).TextColor(s.muted)
		ui.Column(c).Grow(1).MinWidth(0).Gap(3).Children(func() {
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, p.Name).FontSize(14.5).FontWeight(600).SingleLine().MinWidth(0)
				kind := tr("Local")
				switch {
				case p.Remote():
					kind = tr("Subscription")
				case p.ID == "":
					kind = tr("Built-in")
				}
				tag(c, s, kind)
				if active {
					ui.Text(c, tr("Active")).FontSize(11).FontWeight(600).Padding(2, 6).Radius(4).Background(s.accent).TextColor(ui.RGB(255, 255, 255)).SingleLine()
				}
			})
			var facts []string
			if p.Remote() {
				facts = append(facts, p.URL)
			}
			if !p.UpdatedAt.IsZero() {
				facts = append(facts, tr("Updated ")+when(p.UpdatedAt))
			}
			if p.Total > 0 {
				facts = append(facts, fmt.Sprintf("%s / %s", size(p.Upload+p.Download), size(p.Total)))
			}
			if p.Expire > 0 {
				facts = append(facts, tr("Expires ")+time.Unix(p.Expire, 0).Format("2006-01-02"))
			}
			if p.ID == "" {
				facts = append(facts, tr("Every connection goes direct. Add a subscription to use proxies."))
			}
			ui.Text(c, strings.Join(facts, " · ")).FontSize(12).TextColor(s.muted).SingleLine().MinWidth(0)
		})
		if k.sub.updating[p.ID] {
			ui.Spinner(c).Size(16, 16)
		}
		if p.Remote() && roundButton(c, s, "rotate-cw", tr("Update now")).Clicked() {
			k.updateProfile(p.ID)
		}
		if !active && ui.Button(c, tr("Use")).Clicked() {
			k.useProfile(p.ID)
		}
		if p.ID != "" && roundButton(c, s, "trash-2", tr("Delete")).Clicked() {
			k.removeProfile(p)
		}
	})
}
