package core

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"gopkg.in/yaml.v3"
)

// MaxProfileSize bounds a subscription's download.
const MaxProfileSize = 32 << 20

// Profile is a configuration the core can run: downloaded from a
// subscription, or imported from a file.
type Profile struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	// URL is the subscription's, empty for a local file.
	URL       string    `json:"url,omitempty"`
	UpdatedAt time.Time `json:"updatedAt"`
	// AutoUpdateHours updates the subscription that often; 0 never.
	AutoUpdateHours int `json:"autoUpdateHours,omitempty"`
	// Usage is the subscription's traffic, from its subscription-userinfo
	// header.
	Upload   int64 `json:"upload,omitempty"`
	Download int64 `json:"download,omitempty"`
	Total    int64 `json:"total,omitempty"`
	Expire   int64 `json:"expire,omitempty"`
}

// Remote says that the profile is a subscription.
func (p Profile) Remote() bool { return p.URL != "" }

// Due says that the subscription should update now.
func (p Profile) Due(now time.Time) bool {
	return p.Remote() && p.AutoUpdateHours > 0 && now.Sub(p.UpdatedAt) >= time.Duration(p.AutoUpdateHours)*time.Hour
}

// Profiles are the profiles in Dir, listed in Dir/profiles.json. They are
// safe for use from several goroutines: read them with List and Active.
type Profiles struct {
	// LoadErr says why the saved list could not be read, "" when it was.
	LoadErr string
	Dir     string

	mu     sync.Mutex
	items  []Profile
	active string
}

type profilesFile struct {
	Items  []Profile `json:"items"`
	Active string    `json:"active"`
}

// LoadProfiles reads the list in dir.
func LoadProfiles(dir string) *Profiles {
	p := &Profiles{Dir: dir}
	var f profilesFile
	path := filepath.Join(dir, "profiles.json")
	if data, err := os.ReadFile(path); err == nil {
		if err := json.Unmarshal(data, &f); err != nil {
			// Kept aside, so that the next save does not lose it for good.
			kept, _ := KeepCorrupt(path)
			p.LoadErr = fmt.Sprintf("the list of subscriptions could not be read (%v); it was kept as %s", err, kept)
			f = profilesFile{}
		}
	}
	p.items, p.active = f.Items, f.Active
	return p
}

// saveLocked writes the list; the caller holds mu.
func (ps *Profiles) saveLocked() error {
	data, _ := json.MarshalIndent(profilesFile{ps.items, ps.active}, "", "  ")
	return WriteFileAtomic(filepath.Join(ps.Dir, "profiles.json"), data, 0o600)
}

// List is a copy of the profiles.
func (ps *Profiles) List() []Profile {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return append([]Profile(nil), ps.items...)
}

// Active is the active profile's ID, "" for the default.
func (ps *Profiles) Active() string {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	return ps.active
}

// SetActive makes id the active profile.
func (ps *Profiles) SetActive(id string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.active = id
	return ps.saveLocked()
}

// Path is the file of a profile.
func (ps *Profiles) Path(id string) string { return filepath.Join(ps.Dir, id+".yaml") }

// Find returns the profile id, or false.
func (ps *Profiles) Find(id string) (Profile, bool) {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	p, i := ps.findLocked(id)
	return p, i >= 0
}

func (ps *Profiles) findLocked(id string) (Profile, int) {
	for i, p := range ps.items {
		if p.ID == id {
			return p, i
		}
	}
	return Profile{}, -1
}

// ActiveYAML is the active profile's configuration, or DefaultProfile.
func (ps *Profiles) ActiveYAML() []byte {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	if _, i := ps.findLocked(ps.active); i >= 0 {
		if data, err := os.ReadFile(ps.Path(ps.active)); err == nil {
			return data
		}
	}
	return []byte(DefaultProfile)
}

// Errors of subscriptions, for the UI to explain.
var (
	// ErrNotClash is data that is no Clash configuration.
	ErrNotClash = errors.New("not a Clash configuration")
	// ErrURIList is a list of share links (vmess://, ss://, …), plain or in
	// base64, which providers serve to clients they do not recognize.
	ErrURIList = errors.New("a list of share links, not a Clash configuration")
	// ErrBadLink is a link that is not http(s).
	ErrBadLink = errors.New("the link must start with https://")
	// ErrTooLarge is a subscription over MaxProfileSize.
	ErrTooLarge = errors.New("larger than the size limit")
)

// Validate checks that data looks like a Clash configuration.
func Validate(data []byte) error {
	if URIList(data) {
		return ErrURIList
	}
	m := map[string]any{}
	if err := yaml.Unmarshal(data, &m); err != nil {
		return fmt.Errorf("%w: %v", ErrNotClash, err)
	}
	for _, k := range []string{"proxies", "proxy-providers", "proxy-groups", "rules", "rule-providers"} {
		if _, ok := m[k]; ok {
			return nil
		}
	}
	return fmt.Errorf("%w: no proxies, groups or rules", ErrNotClash)
}

var shareSchemes = []string{"vmess://", "vless://", "ss://", "ssr://", "trojan://", "hysteria://", "hysteria2://", "hy2://", "tuic://", "wireguard://", "socks5://", "http://", "https://", "anytls://"}

// URIList says whether data is a list of share links, plain or base64.
func URIList(data []byte) bool {
	text := strings.TrimSpace(string(data))
	if decoded, ok := decodeBase64(text); ok {
		text = decoded
	}
	lines, links := 0, 0
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		lines++
		for _, sch := range shareSchemes {
			if strings.HasPrefix(strings.ToLower(line), sch) {
				links++
				break
			}
		}
	}
	return lines > 0 && links*2 > lines
}

func decodeBase64(s string) (string, bool) {
	s = strings.Join(strings.Fields(s), "")
	if s == "" || strings.ContainsAny(s, ":{}") {
		return "", false
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if b, err := enc.DecodeString(s); err == nil {
			return string(b), true
		}
	}
	return "", false
}

func newID() string { return strconv.FormatInt(time.Now().UnixNano(), 36) }

// ErrInsecureRedirect is a subscription that redirects from https to http.
var ErrInsecureRedirect = errors.New("the subscription redirected from https to http")

// fetchClient downloads subscriptions: directly, never through the system
// proxy (which may be Kumo itself), with a timeout and few redirects.
var fetchClient = &http.Client{
	Timeout: 60 * time.Second,
	Transport: &http.Transport{
		Proxy:                 nil,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
	},
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("too many redirects")
		}
		if via[0].URL.Scheme == "https" && req.URL.Scheme != "https" {
			return ErrInsecureRedirect
		}
		return nil
	},
}

// Fetch downloads a subscription, returning its configuration, a name from
// its headers, and its usage.
func Fetch(ctx context.Context, rawURL string) (data []byte, name string, usage Profile, err error) {
	u, err := url.Parse(strings.TrimSpace(rawURL))
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, "", usage, ErrBadLink
	}
	req, err := http.NewRequestWithContext(ctx, "GET", u.String(), nil)
	if err != nil {
		return nil, "", usage, err
	}
	// Subscription services send the Clash format to Clash's agents.
	req.Header.Set("User-Agent", "clash.meta/mihomo Kumo")
	res, err := fetchClient.Do(req)
	if err != nil {
		return nil, "", usage, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, "", usage, fmt.Errorf("subscription: %s", res.Status)
	}
	data, err = io.ReadAll(io.LimitReader(res.Body, MaxProfileSize+1))
	if err != nil {
		return nil, "", usage, err
	}
	if len(data) > MaxProfileSize {
		return nil, "", usage, ErrTooLarge
	}
	if err := Validate(data); err != nil {
		return nil, "", usage, err
	}
	if _, params, err := mime.ParseMediaType(res.Header.Get("Content-Disposition")); err == nil {
		name = strings.TrimSuffix(strings.TrimSuffix(params["filename"], ".yaml"), ".yml")
	}
	if name == "" {
		name = u.Host
	}
	for _, kv := range strings.Split(res.Header.Get("Subscription-Userinfo"), ";") {
		k, v, _ := strings.Cut(strings.TrimSpace(kv), "=")
		n, _ := strconv.ParseInt(strings.TrimSpace(v), 10, 64)
		switch k {
		case "upload":
			usage.Upload = n
		case "download":
			usage.Download = n
		case "total":
			usage.Total = n
		case "expire":
			usage.Expire = n
		}
	}
	return data, name, usage, nil
}

// AddRemote downloads a subscription and adds it.
func (ps *Profiles) AddRemote(ctx context.Context, rawURL, name string, autoUpdateHours int) (Profile, error) {
	data, got, usage, err := Fetch(ctx, rawURL)
	if err != nil {
		return Profile{}, err
	}
	if name == "" {
		name = got
	}
	p := usage
	p.ID, p.Name, p.URL, p.UpdatedAt, p.AutoUpdateHours = newID(), name, strings.TrimSpace(rawURL), time.Now(), autoUpdateHours
	return p, ps.add(p, data)
}

// AddLocal imports a configuration file.
func (ps *Profiles) AddLocal(path string) (Profile, error) {
	st, err := os.Stat(path)
	if err != nil {
		return Profile{}, err
	}
	if st.Size() > MaxProfileSize {
		return Profile{}, ErrTooLarge
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return Profile{}, err
	}
	if err := Validate(data); err != nil {
		return Profile{}, err
	}
	name := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	p := Profile{ID: newID(), Name: name, UpdatedAt: time.Now()}
	return p, ps.add(p, data)
}

func (ps *Profiles) add(p Profile, data []byte) error {
	if err := WriteFileAtomic(ps.Path(p.ID), data, 0o600); err != nil {
		return err
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	ps.items = append(ps.items, p)
	if ps.active == "" {
		ps.active = p.ID
	}
	return ps.saveLocked()
}

// Update downloads a subscription anew.
func (ps *Profiles) Update(ctx context.Context, id string) error {
	p, ok := ps.Find(id)
	if !ok || !p.Remote() {
		return errors.New("not a subscription")
	}
	data, _, usage, err := Fetch(ctx, p.URL)
	if err != nil {
		return err
	}
	if err := WriteFileAtomic(ps.Path(id), data, 0o600); err != nil {
		return err
	}
	ps.mu.Lock()
	defer ps.mu.Unlock()
	cur, i := ps.findLocked(id)
	if i < 0 {
		return nil // removed meanwhile
	}
	cur.UpdatedAt = time.Now()
	cur.Upload, cur.Download, cur.Total, cur.Expire = usage.Upload, usage.Download, usage.Total, usage.Expire
	ps.items[i] = cur
	return ps.saveLocked()
}

// Remove deletes a profile; the active one falls back to the default.
func (ps *Profiles) Remove(id string) error {
	ps.mu.Lock()
	defer ps.mu.Unlock()
	_, i := ps.findLocked(id)
	if i < 0 {
		return nil
	}
	ps.items = append(ps.items[:i], ps.items[i+1:]...)
	os.Remove(ps.Path(id))
	if ps.active == id {
		ps.active = ""
	}
	return ps.saveLocked()
}
