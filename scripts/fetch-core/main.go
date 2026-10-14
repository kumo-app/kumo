// Command fetch-core puts the mihomo core pinned in core.lock.json into
// resources/<os>-<arch>/core/, which mygo ships with that platform's build.
// Every download is checked against its pinned SHA-256 before it is used.
//
//	go run ./scripts/fetch-core                         # this machine
//	go run ./scripts/fetch-core -platform darwin/universal,windows/amd64
//	go run ./scripts/fetch-core -all
package main

import (
	"archive/zip"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
)

//go:embed core.lock.json
var lockFile []byte

type pin struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	// Binary is the SHA-256 of the extracted executable, which Kumo
	// embeds (package corepin) to trust a core before privileging it.
	Binary string `json:"binary"`
}

type lockData struct {
	Repo    string         `json:"repo"`
	Version string         `json:"version"`
	Assets  map[string]pin `json:"assets"`
}

func main() {
	platforms := flag.String("platform", runtime.GOOS+"/"+runtime.GOARCH, "comma-separated os/arch list; darwin/universal means both Mac architectures")
	all := flag.Bool("all", false, "fetch every pinned platform")
	out := flag.String("out", "resources", "resources directory")
	flag.Parse()

	var lk lockData
	if err := json.Unmarshal(lockFile, &lk); err != nil {
		fail(err)
	}
	var targets []string
	if *all {
		for k := range lk.Assets {
			targets = append(targets, k)
		}
		sort.Strings(targets)
	} else {
		for _, p := range strings.Split(*platforms, ",") {
			p = strings.ReplaceAll(strings.TrimSpace(p), "/", "-")
			if p == "darwin-universal" {
				targets = append(targets, "darwin-amd64", "darwin-arm64")
			} else if p != "" {
				targets = append(targets, p)
			}
		}
	}
	for _, t := range targets {
		a, ok := lk.Assets[t]
		if !ok {
			fail(fmt.Errorf("no core pinned for %s", t))
		}
		if err := install(lk, t, a, *out); err != nil {
			fail(fmt.Errorf("%s: %w", t, err))
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "fetch-core:", err)
	os.Exit(1)
}

func install(lk lockData, target string, a pin, out string) error {
	name := "mihomo"
	if strings.HasPrefix(target, "windows-") {
		name += ".exe"
	}
	dest := filepath.Join(out, target, "core", name)
	// The stamp lives outside resources/ so it is never packaged with the app.
	stamp := filepath.Join(out, "..", "build", "core-stamps", target+".sha256")
	if b, err := os.ReadFile(stamp); err == nil && strings.TrimSpace(string(b)) == a.SHA256 {
		if _, err := os.Stat(dest); err == nil {
			fmt.Printf("%s: %s %s already present\n", target, lk.Version, name)
			return nil
		}
	}
	url := fmt.Sprintf("https://github.com/%s/releases/download/%s/%s", lk.Repo, lk.Version, a.File)
	fmt.Printf("%s: downloading %s\n", target, url)
	archive, err := download(url)
	if err != nil {
		return err
	}
	sum := sha256.Sum256(archive)
	if got := hex.EncodeToString(sum[:]); got != a.SHA256 {
		return fmt.Errorf("checksum mismatch: got %s, want %s", got, a.SHA256)
	}
	bin, err := unpack(a.File, archive)
	if err != nil {
		return err
	}
	if sum := sha256.Sum256(bin); hex.EncodeToString(sum[:]) != a.Binary {
		return fmt.Errorf("extracted core checksum mismatch: got %x, want %s", sum, a.Binary)
	}
	if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
		return err
	}
	tmp := dest + ".part"
	if err := os.WriteFile(tmp, bin, 0o755); err != nil {
		return err
	}
	if err := os.Rename(tmp, dest); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(stamp), 0o755); err != nil {
		return err
	}
	return os.WriteFile(stamp, []byte(a.SHA256+"\n"), 0o644)
}

func download(url string) ([]byte, error) {
	client := &http.Client{Timeout: 5 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 256<<20))
}

// unpack returns the executable inside a .gz or .zip release asset.
func unpack(file string, data []byte) ([]byte, error) {
	switch {
	case strings.HasSuffix(file, ".gz"):
		r, err := gzip.NewReader(bytes.NewReader(data))
		if err != nil {
			return nil, err
		}
		return io.ReadAll(r)
	case strings.HasSuffix(file, ".zip"):
		z, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return nil, err
		}
		for _, f := range z.File {
			if strings.HasSuffix(strings.ToLower(f.Name), ".exe") {
				r, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer r.Close()
				return io.ReadAll(r)
			}
		}
		return nil, errors.New("no .exe in the zip")
	}
	return nil, fmt.Errorf("unknown archive type: %s", file)
}
