package update

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Feed is where updates come from.
type Feed struct {
	// Repo is the GitHub repository, "kumo-app/kumo".
	Repo string
	// Web and API default to https://github.com and https://api.github.com.
	Web, API string
	// PublicKey defaults to the package's PublicKey.
	PublicKey string
	// Proxy, when set, picks the proxy of each request (e.g. Kumo's own
	// port while it is connected).
	Proxy func(*http.Request) (*url.URL, error)
	// UserAgent is sent with every request.
	UserAgent string
}

func (f *Feed) web() string {
	if f.Web != "" {
		return strings.TrimRight(f.Web, "/")
	}
	return "https://github.com"
}

func (f *Feed) api() string {
	if f.API != "" {
		return strings.TrimRight(f.API, "/")
	}
	return "https://api.github.com"
}

func (f *Feed) key() string {
	if f.PublicKey != "" {
		return f.PublicKey
	}
	return PublicKey
}

func (f *Feed) client(timeout time.Duration) *http.Client {
	tr := http.DefaultTransport.(*http.Transport).Clone()
	if f.Proxy != nil {
		tr.Proxy = f.Proxy
	}
	return &http.Client{Transport: tr, Timeout: timeout}
}

func (f *Feed) get(ctx context.Context, c *http.Client, u string, limit int64) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	if f.UserAgent != "" {
		req.Header.Set("User-Agent", f.UserAgent)
	}
	resp, err := c.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, &StatusError{URL: u, Code: resp.StatusCode}
	}
	return io.ReadAll(io.LimitReader(resp.Body, limit))
}

// StatusError is an HTTP answer other than 200.
type StatusError struct {
	URL  string
	Code int
}

func (e *StatusError) Error() string { return fmt.Sprintf("update: %s: HTTP %d", e.URL, e.Code) }

// manifestAt fetches and verifies the manifest at u and its signature at
// u+".sig".
func (f *Feed) manifestAt(ctx context.Context, c *http.Client, u string) (*Manifest, error) {
	data, err := f.get(ctx, c, u, 1<<20)
	if err != nil {
		return nil, err
	}
	sig, err := f.get(ctx, c, u+".sig", 4096)
	if err != nil {
		return nil, err
	}
	return ParseManifest(data, string(sig), f.key())
}

// Latest returns the newest release of channel: Stable reads the latest
// release; Beta the newest of the recent releases, pre-releases included.
func (f *Feed) Latest(ctx context.Context, channel string) (*Manifest, error) {
	c := f.client(30 * time.Second)
	if channel != Beta {
		return f.manifestAt(ctx, c, f.web()+"/"+f.Repo+"/releases/latest/download/latest.json")
	}
	data, err := f.get(ctx, c, f.api()+"/repos/"+f.Repo+"/releases?per_page=20", 4<<20)
	if err != nil {
		return nil, err
	}
	var rels []struct {
		Tag    string `json:"tag_name"`
		Draft  bool   `json:"draft"`
		Assets []struct {
			Name string `json:"name"`
			URL  string `json:"browser_download_url"`
		} `json:"assets"`
	}
	if err := json.Unmarshal(data, &rels); err != nil {
		return nil, err
	}
	var best *Manifest
	var lastErr error
	for _, r := range rels {
		if r.Draft || (best != nil && !Newer(r.Tag, best.Version)) {
			continue
		}
		for _, a := range r.Assets {
			if a.Name != "latest.json" {
				continue
			}
			m, err := f.manifestAt(ctx, c, a.URL)
			if err != nil {
				lastErr = err
				continue
			}
			if best == nil || Newer(m.Version, best.Version) {
				best = m
			}
		}
	}
	if best == nil {
		if lastErr == nil {
			lastErr = errors.New("update: no release has a manifest")
		}
		return nil, lastErr
	}
	return best, nil
}

// Download fetches asset a into dir, reporting progress, and keeps it
// only if VerifyFile accepts it. A partial download resumes.
func (f *Feed) Download(ctx context.Context, a Asset, dir string, progress func(done, total int64)) (string, error) {
	if a.Name == "" || strings.ContainsAny(a.Name, `/\`) || a.Name == "." || a.Name == ".." {
		return "", fmt.Errorf("update: bad asset name %q", a.Name)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	final := filepath.Join(dir, a.Name)
	if VerifyFile(final, a, f.key()) == nil {
		if progress != nil {
			progress(a.Size, a.Size)
		}
		return final, nil
	}
	part := final + ".part"
	var have int64
	if st, err := os.Stat(part); err == nil && (a.Size == 0 || st.Size() < a.Size) {
		have = st.Size()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, a.URL, nil)
	if err != nil {
		return "", err
	}
	if f.UserAgent != "" {
		req.Header.Set("User-Agent", f.UserAgent)
	}
	if have > 0 {
		req.Header.Set("Range", fmt.Sprintf("bytes=%d-", have))
	}
	resp, err := f.client(0).Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	flags := os.O_CREATE | os.O_WRONLY
	switch resp.StatusCode {
	case http.StatusPartialContent:
		flags |= os.O_APPEND
	case http.StatusOK:
		have = 0
		flags |= os.O_TRUNC
	default:
		return "", &StatusError{URL: a.URL, Code: resp.StatusCode}
	}
	out, err := os.OpenFile(part, flags, 0o600)
	if err != nil {
		return "", err
	}
	total := a.Size
	buf := make([]byte, 256<<10)
	done := have
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if a.Size > 0 && done+int64(n) > a.Size {
				out.Close()
				os.Remove(part)
				return "", fmt.Errorf("update: %s is larger than its manifest says", a.Name)
			}
			if _, err := out.Write(buf[:n]); err != nil {
				out.Close()
				return "", err
			}
			done += int64(n)
			if progress != nil {
				progress(done, total)
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			out.Close()
			return "", rerr
		}
	}
	if err := out.Close(); err != nil {
		return "", err
	}
	if err := VerifyFile(part, a, f.key()); err != nil {
		os.Remove(part)
		return "", err
	}
	if err := os.Rename(part, final); err != nil {
		return "", err
	}
	return final, nil
}
