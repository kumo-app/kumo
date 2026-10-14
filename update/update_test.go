package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func TestCompare(t *testing.T) {
	order := []string{"0.4.4-alpha", "0.4.4-alpha.1", "0.4.4-alpha.beta", "0.4.4-beta", "0.4.4-beta.2", "0.4.4-beta.11", "0.4.4-rc.1", "0.4.4", "0.4.5", "0.10.0", "1.0.0"}
	for i := range order {
		for j := range order {
			a, _ := ParseVersion(order[i])
			b, _ := ParseVersion(order[j])
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := Compare(a, b); got != want {
				t.Errorf("Compare(%s, %s) = %d, want %d", order[i], order[j], got, want)
			}
		}
	}
	if !Newer("v0.4.5", "0.4.4") || Newer("0.4.4", "0.4.4") || Newer("garbage", "0.4.4") || Newer("0.4.5-beta.1", "0.4.5") {
		t.Fatal("Newer")
	}
	for _, bad := range []string{"", "1.2", "1.2.x", "1.2.3-", "-1.2.3"} {
		if _, err := ParseVersion(bad); err == nil {
			t.Errorf("ParseVersion(%q) accepted", bad)
		}
	}
}

type keys struct {
	pub  string
	priv ed25519.PrivateKey
}

func newKeys(t *testing.T) keys {
	pub, priv, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return keys{base64.StdEncoding.EncodeToString(pub), priv}
}

func (k keys) sign(b []byte) string { return base64.StdEncoding.EncodeToString(ed25519.Sign(k.priv, b)) }

func (k keys) asset(name string, data []byte, url string) Asset {
	s := sha256.Sum256(data)
	h := hex.EncodeToString(s[:])
	return Asset{Name: name, URL: url, Size: int64(len(data)), SHA256: h, Sig: k.sign(AssetMessage(name, h))}
}

func TestManifestSignature(t *testing.T) {
	k := newKeys(t)
	data, _ := json.Marshal(Manifest{Version: "0.5.0", Channel: Stable})
	if _, err := ParseManifest(data, k.sign(data), k.pub); err != nil {
		t.Fatal(err)
	}
	tampered := bytes.Replace(data, []byte("0.5.0"), []byte("9.5.0"), 1)
	if _, err := ParseManifest(tampered, k.sign(data), k.pub); err == nil {
		t.Fatal("a tampered manifest verified")
	}
	other := newKeys(t)
	if _, err := ParseManifest(data, other.sign(data), k.pub); err == nil {
		t.Fatal("a manifest signed by another key verified")
	}
	if _, err := publicKey(PublicKey); err != nil {
		t.Fatal("the built-in public key is invalid")
	}
}

func TestVerifyFile(t *testing.T) {
	k := newKeys(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "a.zip")
	data := []byte("payload")
	os.WriteFile(p, data, 0o600)
	a := k.asset("a.zip", data, "")
	if err := VerifyFile(p, a, k.pub); err != nil {
		t.Fatal(err)
	}
	renamed := a
	renamed.Name = "b.zip" // the signature binds the name
	if VerifyFile(p, renamed, k.pub) == nil {
		t.Fatal("a signature for another name verified")
	}
	os.WriteFile(p, []byte("payloaX"), 0o600)
	if VerifyFile(p, a, k.pub) == nil {
		t.Fatal("altered bytes verified")
	}
	// A matching hash whose signature is forged.
	forged := a
	forged.Sig = newKeys(t).sign(AssetMessage(a.Name, a.SHA256))
	os.WriteFile(p, data, 0o600)
	if VerifyFile(p, forged, k.pub) == nil {
		t.Fatal("a forged signature verified")
	}
}

// server serves a fake GitHub: releases with manifests and assets.
func server(t *testing.T, k keys, files map[string][]byte, releases string) (*httptest.Server, *atomic.Int64) {
	var ranged atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/api/repos/o/r/releases" {
			io.WriteString(w, releases)
			return
		}
		data, ok := files[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Range") != "" {
			ranged.Add(1)
		}
		http.ServeContent(w, r, "", time.Time{}, bytes.NewReader(data))
	}))
	t.Cleanup(srv.Close)
	return srv, &ranged
}

func TestLatestAndDownload(t *testing.T) {
	k := newKeys(t)
	files := map[string][]byte{}
	var srv *httptest.Server
	publish := func(tag, ver, ch string, payload []byte) {
		name := "kumo-" + ver + "-linux-amd64.tar.gz"
		files["/o/r/releases/download/"+tag+"/"+name] = payload
		m := Manifest{Version: ver, Channel: ch, Assets: map[string]Asset{"linux-amd64-tar": k.asset(name, payload, srv.URL+"/o/r/releases/download/"+tag+"/"+name)}}
		data, _ := json.Marshal(m)
		files["/o/r/releases/download/"+tag+"/latest.json"] = data
		files["/o/r/releases/download/"+tag+"/latest.json.sig"] = []byte(k.sign(data))
		if ch == Stable {
			files["/o/r/releases/latest/download/latest.json"] = data
			files["/o/r/releases/latest/download/latest.json.sig"] = []byte(k.sign(data))
		}
	}
	srv, ranged := server(t, k, files, "")
	payload := bytes.Repeat([]byte("kumo"), 50000)
	publish("v0.5.0", "0.5.0", Stable, payload)
	publish("v0.6.0-beta.1", "0.6.0-beta.1", Beta, []byte("beta"))
	rel := fmt.Sprintf(`[{"tag_name":"v0.6.0-beta.1","assets":[{"name":"latest.json","browser_download_url":"%[1]s/o/r/releases/download/v0.6.0-beta.1/latest.json"}]},
		{"tag_name":"v0.7.0","draft":true,"assets":[]},
		{"tag_name":"v0.5.0","assets":[{"name":"latest.json","browser_download_url":"%[1]s/o/r/releases/download/v0.5.0/latest.json"}]}]`, srv.URL)
	srv2, _ := server(t, k, files, rel)
	f := &Feed{Repo: "o/r", Web: srv2.URL, API: srv2.URL + "/api", PublicKey: k.pub}

	m, err := f.Latest(context.Background(), Stable)
	if err != nil || m.Version != "0.5.0" {
		t.Fatalf("stable: %v %v", m, err)
	}
	b, err := f.Latest(context.Background(), Beta)
	if err != nil || b.Version != "0.6.0-beta.1" {
		t.Fatalf("beta: %v %v", b, err)
	}

	dir := t.TempDir()
	a := m.Assets["linux-amd64-tar"]
	// A partial file from an earlier attempt is resumed.
	os.WriteFile(filepath.Join(dir, a.Name+".part"), payload[:1000], 0o600)
	var last int64
	p, err := f.Download(context.Background(), a, dir, func(done, total int64) { last = done })
	if err != nil {
		t.Fatal(err)
	}
	if last != a.Size || ranged.Load() != 1 {
		t.Fatalf("progress %d of %d, ranged %d", last, a.Size, ranged.Load())
	}
	if got, _ := os.ReadFile(p); !bytes.Equal(got, payload) {
		t.Fatal("download differs")
	}

	// A file that does not match its manifest is dropped.
	bad := a
	bad.SHA256 = strings.Repeat("0", 64)
	if _, err := f.Download(context.Background(), bad, t.TempDir(), nil); err == nil {
		t.Fatal("a mismatching download was kept")
	}

	// A manifest signed with another key is refused.
	f.PublicKey = newKeys(t).pub
	if _, err := f.Latest(context.Background(), Stable); err == nil {
		t.Fatal("a manifest signed by another key was accepted")
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		exe, goos string
		kind      Kind
		target    string
		manual    string
	}{
		{"/Applications/Kumo.app/Contents/MacOS/Kumo", "darwin", MacApp, "/Applications/Kumo.app", ""},
		{"/private/var/folders/x/AppTranslocation/ABC/d/Kumo.app/Contents/MacOS/Kumo", "darwin", MacApp, "/private/var/folders/x/AppTranslocation/ABC/d/Kumo.app", ReasonTranslocated},
		{"/home/u/apps/kumo/kumo", "linux", LinuxTar, "/home/u/apps/kumo", ""},
		{"/opt/kumo/kumo", "linux", LinuxDeb, "/opt/kumo", ReasonPackage},
		{"/usr/local/bin/kumo", "darwin", "", "", ReasonUnknown},
	}
	for _, c := range cases {
		in := Detect(c.exe, c.goos)
		if in.Kind != c.kind || in.Target != c.target || in.Manual != c.manual {
			t.Errorf("Detect(%s) = %+v", c.exe, in)
		}
	}
	if Key("darwin", "arm64", MacApp) != "darwin-universal-app" || Key("windows", "amd64", WinSetup) != "windows-amd64-setup" {
		t.Fatal("Key")
	}
	for name, key := range map[string]string{
		"Kumo-0.5.0-macos-universal.app.zip": "darwin-universal-app", "Kumo-Setup-0.5.0-amd64.exe": "windows-amd64-setup",
		"Kumo-0.5.0-windows-amd64-portable.zip": "windows-amd64-portable", "kumo-0.5.0-linux-arm64.tar.gz": "linux-arm64-tar",
		"kumo_0.5.0_amd64.deb": "linux-amd64-deb", "Kumo-0.5.0.dmg": "", "SHA256SUMS": "",
	} {
		if got := AssetKeyFor(name); got != key {
			t.Errorf("AssetKeyFor(%s) = %q, want %q", name, got, key)
		}
	}
}

func makeTar(t *testing.T, entries map[string]string) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range entries {
		tw.WriteHeader(&tar.Header{Name: name, Mode: 0o755, Size: int64(len(body)), Typeflag: tar.TypeReg})
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "a.tar.gz")
	os.WriteFile(p, buf.Bytes(), 0o600)
	return p
}

func TestArchivesStayInside(t *testing.T) {
	if err := Untar(makeTar(t, map[string]string{"../evil": "x"}), t.TempDir()); err == nil {
		t.Fatal("tar escaped its folder")
	}
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("../../evil")
	w.Write([]byte("x"))
	zw.Close()
	p := filepath.Join(t.TempDir(), "a.zip")
	os.WriteFile(p, buf.Bytes(), 0o600)
	if err := Unzip(p, t.TempDir()); err == nil {
		t.Fatal("zip escaped its folder")
	}
}

type fakeProc struct{ exited bool }

func (f *fakeProc) Exited() bool { return f.exited }
func (f *fakeProc) Kill()        {}

// installed lays out a tar install of version v under a fresh parent.
func installed(t *testing.T, v string) Install {
	parent := t.TempDir()
	target := filepath.Join(parent, "kumo")
	os.MkdirAll(filepath.Join(target, "core"), 0o755)
	os.WriteFile(filepath.Join(target, "kumo"), []byte(v), 0o755)
	os.WriteFile(filepath.Join(target, "notes.txt"), []byte("mine"), 0o644)
	return Install{Kind: LinuxTar, Target: target, Exe: "kumo"}
}

func runPlan(t *testing.T, in Install, newStarts bool) Result {
	tgz := makeTar(t, map[string]string{"kumo": "new", "core/mihomo": "core"})
	p, err := Prepare(in, tgz, "0.5.0", t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p.Wait = 2 * time.Second
	defer func(l func(string, []string) (proc, error), w func(int, time.Duration) bool) { launch, waitExit = l, w }(launch, waitExit)
	waitExit = func(int, time.Duration) bool { return true }
	var launched []string
	launch = func(exe string, env []string) (proc, error) {
		body, _ := os.ReadFile(exe)
		launched = append(launched, string(body))
		for _, kv := range env {
			if ok, found := strings.CutPrefix(kv, OKEnv+"="); found && newStarts {
				os.WriteFile(ok, nil, 0o600)
			}
		}
		return &fakeProc{exited: !newStarts}, nil
	}
	r := apply(p, log.New(io.Discard, "", 0))
	t.Logf("launched %v", launched)
	return r
}

func TestApplySwapsAndKeepsUserFiles(t *testing.T) {
	in := installed(t, "old")
	r := runPlan(t, in, true)
	if !r.OK {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(in.Target, "kumo")); string(b) != "new" {
		t.Fatalf("exe is %q", b)
	}
	if b, _ := os.ReadFile(filepath.Join(in.Target, "notes.txt")); string(b) != "mine" {
		t.Fatal("the user's file was lost")
	}
	if _, err := os.Stat(in.Target + ".old"); !os.IsNotExist(err) {
		t.Fatal("the old copy was left behind")
	}
	if _, err := os.Stat(stageDir(in.Target)); !os.IsNotExist(err) {
		t.Fatal("the staging folder was left behind")
	}
}

func TestApplyRollsBack(t *testing.T) {
	in := installed(t, "old")
	r := runPlan(t, in, false)
	if r.OK || !r.RolledBack {
		t.Fatalf("%+v", r)
	}
	if b, _ := os.ReadFile(filepath.Join(in.Target, "kumo")); string(b) != "old" {
		t.Fatalf("exe after rollback is %q", b)
	}
	entries, _ := os.ReadDir(filepath.Dir(in.Target))
	if len(entries) != 1 {
		t.Fatalf("left behind: %v", entries)
	}
}

func TestResultRoundTrip(t *testing.T) {
	w := t.TempDir()
	data, _ := json.Marshal(Result{Version: "0.5.0", OK: true})
	os.WriteFile(ResultFile(w), data, 0o600)
	r, ok := TakeResult(w)
	if !ok || !r.OK || r.Version != "0.5.0" {
		t.Fatal(r)
	}
	if _, ok := TakeResult(w); ok {
		t.Fatal("the result was not removed")
	}
}
