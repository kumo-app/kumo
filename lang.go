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

// MyGo's locale for each form of Chinese characters. The locale is the
// language of the text engine too, so it picks the CJK glyph variant, and
// it picks the catalog of MyGo's own strings (menus, dialogs, controls):
// zh-Hant for Taiwan and Hong Kong, zh-Hans for Simplified.
const (
	localeTW = "zh-Hant-TW" // Taiwan forms
	localeHK = "zh-Hant-HK" // Hong Kong forms
	localeCN = "zh-Hans-CN" // Simplified forms
)

// mygoLanguages are the languages Kumo declares to MyGo (App.SetLanguages).
var mygoLanguages = []string{"en", localeTW, localeHK, localeCN}

// mygoLocale is the locale MyGo shows for Kumo's language l. English is
// English. Chinese follows the first Chinese language of the system's
// preferred ones (App.PreferredLanguages, which SetLocale leaves alone):
// Taiwan forms for zh-TW or zh-Hant, Hong Kong forms for zh-HK or zh-MO,
// Simplified forms for zh-CN, zh-SG or zh-Hans. A Chinese language that
// names no region (as macOS's "zh-Hant") takes the region of the system's
// formats, region (App.Region().Locale, as "zh-HK" or "en-HK"). Without a
// Chinese language, the forms of Kumo's own Chinese, Traditional as
// written in Taiwan.
func mygoLocale(l string, preferred []string, region string) string {
	if l != "zh" {
		return "en"
	}
	for _, p := range preferred {
		if f, ok := chineseForms(p, regionOf(region)); ok {
			return f
		}
	}
	return localeTW
}

// regionOf returns the region subtag of a locale, lower case, or "".
func regionOf(tag string) string {
	_, region := tagParts(tag)
	return region
}

// tagParts splits a tag, as "zh-Hant-HK" or "zh_TW.UTF-8", into its lower
// case subtags, the language first, and its region subtag.
func tagParts(tag string) ([]string, string) {
	tag, _, _ = strings.Cut(tag, ".")
	tag, _, _ = strings.Cut(tag, "@")
	parts := strings.FieldsFunc(strings.ToLower(strings.TrimSpace(tag)), func(r rune) bool { return r == '-' || r == '_' })
	for _, s := range parts[min(1, len(parts)):] {
		if len(s) == 2 || len(s) == 3 && s[0] >= '0' && s[0] <= '9' {
			return parts, s
		}
	}
	return parts, ""
}

// chineseForms reads a language tag of the system, as "zh-Hant-HK",
// "zh_TW.UTF-8" or "yue-HK": for a Chinese one, the locale of its
// character forms. A tag without a region takes fallback's.
func chineseForms(tag, fallback string) (string, bool) {
	parts, region := tagParts(tag)
	if len(parts) == 0 || parts[0] != "zh" && parts[0] != "yue" && parts[0] != "cmn" {
		return "", false
	}
	script := ""
	for _, s := range parts[1:] {
		if len(s) == 4 {
			script = s
			break
		}
	}
	if region == "" {
		region = fallback
	}
	hk := region == "hk" || region == "mo"
	switch {
	case script == "hans":
		return localeCN, true
	case script == "hant" && hk:
		return localeHK, true
	case script == "hant":
		return localeTW, true
	case hk:
		return localeHK, true
	case region == "tw":
		return localeTW, true
	case region == "cn" || region == "sg" || region == "my":
		return localeCN, true
	case parts[0] == "yue":
		return localeHK, true // Cantonese is written with Hong Kong forms
	}
	// Chinese with no script or known region ("zh"): Kumo's own forms.
	return localeTW, true
}
