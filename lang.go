package main

import (
	"slices"
	"strings"
)

// lang is the language of the interface: "zh" (Traditional Chinese) or
// "en". English strings are the keys; zhTW translates them.
var lang = "zh"

func tr(s string) string {
	if lang == "zh" {
		if t, ok := zhTW[s]; ok {
			return t
		}
	}
	return s
}

// languages are the interface languages, as stored in the preferences.
var languages = []string{"zh", "en"}

// languageFor maps a system locale (macOS AppleLanguages entries such as
// "zh-Hant-TW", "zh-Hans", "en-GB"; LANG values such as "zh_TW.UTF-8") to
// an interface language. Every Chinese variant gets Kumo's Chinese;
// anything else English.
func languageFor(locale string) string {
	l := strings.ToLower(strings.TrimSpace(locale))
	l, _, _ = strings.Cut(l, ".")
	l = strings.ReplaceAll(l, "_", "-")
	if l == "zh" || strings.HasPrefix(l, "zh-") {
		return "zh"
	}
	return "en"
}

// chooseLanguage keeps a stored language Kumo supports; otherwise (first
// launch, or a value from an older version such as "" or "system") it
// follows the user's preferred languages, most preferred first: the first
// that is Chinese or English decides, and English when none is.
//
// MyGo 0.5's App.Locale picks among the languages an app declares, so it
// cannot tell a reader of zh-Hans from one of English when Kumo declares
// zh-Hant; the preferred languages can.
func chooseLanguage(stored string, preferred []string) string {
	if slices.Contains(languages, stored) {
		return stored
	}
	for _, p := range preferred {
		l := strings.ToLower(strings.TrimSpace(p))
		if l == "en" || strings.HasPrefix(l, "en-") || strings.HasPrefix(l, "en_") {
			return "en"
		}
		if languageFor(p) == "zh" {
			return "zh"
		}
	}
	return "en"
}

// mygoLocale is the language MyGo's own strings (its menus, dialogs and
// controls) are said in, to match Kumo's.
func mygoLocale(l string) string {
	if l == "zh" {
		return "zh-Hant"
	}
	return "en"
}
