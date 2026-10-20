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
}

// The glyph forms (and MyGo's own strings) of Chinese follow the system's
// first Chinese language; English stays English.
func TestMygoLocale(t *testing.T) {
	cases := []struct {
		ui        string
		preferred []string
		region    string
		want      string
	}{
		{"en", []string{"zh-Hant-TW"}, "", "en"},
		{"en", nil, "", "en"},
		{"zh", []string{"zh-Hant-TW"}, "", "zh-Hant-TW"},
		{"zh", []string{"zh-TW"}, "", "zh-Hant-TW"},
		{"zh", []string{"zh_TW.UTF-8"}, "", "zh-Hant-TW"},
		{"zh", []string{"zh-Hant"}, "", "zh-Hant-TW"},
		{"zh", []string{"zh-HK"}, "", "zh-Hant-HK"},
		{"zh", []string{"zh_HK.UTF-8"}, "", "zh-Hant-HK"},
		{"zh", []string{"zh-Hant-HK"}, "", "zh-Hant-HK"},
		{"zh", []string{"zh-MO"}, "", "zh-Hant-HK"},
		{"zh", []string{"zh-Hant-MO"}, "", "zh-Hant-HK"},
		{"zh", []string{"yue-Hant-HK"}, "", "zh-Hant-HK"},
		{"zh", []string{"zh-CN"}, "", "zh-Hans-CN"},
		{"zh", []string{"zh_CN.GB18030"}, "", "zh-Hans-CN"},
		{"zh", []string{"zh-Hans"}, "", "zh-Hans-CN"},
		{"zh", []string{"zh-Hans-CN"}, "", "zh-Hans-CN"},
		{"zh", []string{"zh-SG"}, "", "zh-Hans-CN"},
		{"zh", []string{"zh-Hans-HK"}, "", "zh-Hans-CN"}, // the script decides
		{"zh", []string{"zh"}, "", "zh-Hant-TW"},
		// The first Chinese language decides, whatever comes before it.
		{"zh", []string{"en-US", "ja-JP", "zh-HK", "zh-CN"}, "", "zh-Hant-HK"},
		{"zh", []string{"zh-CN", "zh-TW"}, "", "zh-Hans-CN"},
		// No Chinese on the system: the forms of Kumo's Traditional Chinese.
		{"zh", []string{"en-US", "de-DE"}, "", "zh-Hant-TW"},
		{"zh", nil, "", "zh-Hant-TW"},
		{"zh", []string{"zhx-TW", "en"}, "", "zh-Hant-TW"},
		// A Chinese language without a region takes the formats' region.
		{"zh", []string{"zh-Hant", "en"}, "zh-HK", "zh-Hant-HK"},
		{"zh", []string{"zh-Hant"}, "en-MO", "zh-Hant-HK"},
		{"zh", []string{"zh"}, "zh-CN", "zh-Hans-CN"},
		{"zh", []string{"zh"}, "en-SG", "zh-Hans-CN"},
		{"zh", []string{"zh-Hant"}, "zh-CN", "zh-Hant-TW"}, // the script decides
		{"zh", []string{"zh-Hans"}, "zh-HK", "zh-Hans-CN"},
		{"zh", []string{"zh-TW"}, "zh-HK", "zh-Hant-TW"}, // its own region decides
		{"zh", []string{"en-US"}, "zh-HK", "zh-Hant-TW"}, // a region alone is no Chinese preference
		{"en", []string{"zh-HK"}, "zh-HK", "en"},
	}
	for _, c := range cases {
		if got := mygoLocale(c.ui, c.preferred, c.region); got != c.want {
			t.Errorf("mygoLocale(%q, %q, %q) = %q, want %q", c.ui, c.preferred, c.region, got, c.want)
		}
	}
	// Every locale picked is one Kumo declares to MyGo.
	for _, c := range cases {
		if got := mygoLocale(c.ui, c.preferred, c.region); !slices.Contains(mygoLanguages, got) {
			t.Errorf("%q is not declared in mygoLanguages", got)
		}
	}
}

func TestFreshPrefsHaveNoLanguage(t *testing.T) {
	p := loadPrefs(t.TempDir() + "/settings.json")
	if p.App.Language != "" {
		t.Fatalf("a fresh install must follow the system, got %q", p.App.Language)
	}
}
