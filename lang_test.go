package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"testing"

	"kumo/clash"
)

// TestTranslations checks that every string passed to tr has a
// Traditional Chinese translation.
func TestTranslations(t *testing.T) {
	files, _ := filepath.Glob("*.go")
	call := regexp.MustCompile(`tr\(("(?:[^"\\]|\\.)*")\)`)
	for _, f := range files {
		src, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range call.FindAllSubmatch(src, -1) {
			s, err := strconv.Unquote(string(m[1]))
			if err != nil {
				t.Fatal(err)
			}
			if _, ok := zhTW[s]; !ok {
				t.Errorf("%s: no translation for %q", f, s)
			}
		}
	}
	for _, s := range autoUpdateChoices {
		if _, ok := zhTW[s]; !ok {
			t.Errorf("no translation for %q", s)
		}
	}
}

func TestSizeParts(t *testing.T) {
	for n, want := range map[int64]string{0: "0 B", 1023: "1023 B", 1536: "1.50 KB", 20 << 20: "20.0 MB", 300 << 30: "300 GB"} {
		if got := size(n); got != want {
			t.Errorf("size(%d) = %q, want %q", n, got, want)
		}
	}
}

func TestGroupOrder(t *testing.T) {
	proxies := map[string]clash.Proxy{
		"GLOBAL": {Name: "GLOBAL", Type: "Selector", All: []string{"DIRECT", "Proxy", "Auto"}},
		"Proxy":  {Name: "Proxy", Type: "Selector", All: []string{"Auto", "A"}},
		"Auto":   {Name: "Auto", Type: "URLTest", All: []string{"A"}},
		"A":      {Name: "A", Type: "Http"},
		"DIRECT": {Name: "DIRECT", Type: "Direct"},
	}
	got := groupOrder(proxies)
	want := []string{"Proxy", "Auto", "GLOBAL"}
	if !slices.Equal(got, want) {
		t.Fatalf("groupOrder = %v, want %v", got, want)
	}
}

func TestReadableYAML(t *testing.T) {
	in := `name: "\U0001F1F8\U0001F1EC Singapore \u00e9 \u0022"`
	want := `name: "🇸🇬 Singapore é \u0022"`
	if got := readableYAML(in); got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

func TestLanguageFor(t *testing.T) {
	for locale, want := range map[string]string{
		"zh-Hant-TW": "zh", "zh-Hans-CN": "zh", "zh-Hant": "zh", "zh_TW.UTF-8": "zh", "zh": "zh", "ZH-hk": "zh",
		"en-US": "en", "en-GB": "en", "ja-JP": "en", "": "en", "zhx": "en",
	} {
		if got := languageFor(locale); got != want {
			t.Errorf("languageFor(%q) = %q, want %q", locale, got, want)
		}
	}
}

func TestChooseLanguage(t *testing.T) {
	cases := []struct {
		stored    string
		preferred []string
		want      string
	}{
		{"", []string{"en-US"}, "en"},                // first launch on an English Mac
		{"", []string{"zh-Hant-TW", "en-US"}, "zh"},  // first launch on a Chinese Mac
		{"", []string{"zh-Hans-CN"}, "zh"},           // Simplified readers get Kumo's Chinese too
		{"", []string{"ja-JP", "zh-Hant-TW"}, "zh"},  // the first Kumo speaks decides
		{"", []string{"fr-FR", "en-GB", "zh"}, "en"}, //
		{"", []string{"de-DE"}, "en"},
		{"", nil, "en"},
		{"system", []string{"en-US"}, "en"},  // a value older versions wrote
		{"en", []string{"zh-Hant-TW"}, "en"}, // the user's choice wins
		{"zh", []string{"en-US"}, "zh"},
	}
	for _, c := range cases {
		if got := chooseLanguage(c.stored, c.preferred); got != c.want {
			t.Errorf("chooseLanguage(%q, %q) = %q, want %q", c.stored, c.preferred, got, c.want)
		}
	}
	if mygoLocale("zh") != "zh-Hant" || mygoLocale("en") != "en" {
		t.Fatal("MyGo's strings must follow Kumo's language")
	}
}

func TestFreshPrefsHaveNoLanguage(t *testing.T) {
	p := loadPrefs(t.TempDir() + "/settings.json")
	if p.App.Language != "" {
		t.Fatalf("a fresh install must follow the system, got %q", p.App.Language)
	}
}
