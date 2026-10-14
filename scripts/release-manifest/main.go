// Command release-manifest signs a release for the in-app updater: it
// writes latest.json (every asset's size, SHA-256 and Ed25519 signature,
// plus the release notes from CHANGELOG.md) and latest.json.sig into the
// release folder. The private key is the base64 Ed25519 seed in
// KUMO_UPDATE_KEY; it must match the public key built into Kumo.
package main

import (
	"bufio"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"kumo/update"
)

func main() {
	dir := flag.String("dir", "out", "release folder")
	version := flag.String("version", "", "version, e.g. 0.4.5")
	repo := flag.String("repo", "kumo-app/kumo", "GitHub repository")
	changelog := flag.String("changelog", "CHANGELOG.md", "changelog to take the notes from")
	base := flag.String("base", "https://github.com", "web address of GitHub (another for local tests)")
	flag.Parse()
	seed, err := base64.StdEncoding.DecodeString(strings.TrimSpace(os.Getenv("KUMO_UPDATE_KEY")))
	if err != nil || len(seed) != ed25519.SeedSize {
		log.Fatal("KUMO_UPDATE_KEY must hold a base64 Ed25519 seed")
	}
	key := ed25519.NewKeyFromSeed(seed)
	if base64.StdEncoding.EncodeToString(key.Public().(ed25519.PublicKey)) != update.PublicKey {
		log.Fatal("KUMO_UPDATE_KEY does not match update.PublicKey: Kumo would refuse this release")
	}
	if _, err := update.ParseVersion(*version); err != nil {
		log.Fatal(err)
	}
	tag := "v" + strings.TrimPrefix(*version, "v")
	m, err := build(*base, *dir, strings.TrimPrefix(*version, "v"), tag, *repo, notes(*changelog, strings.TrimPrefix(*version, "v")), key)
	if err != nil {
		log.Fatal(err)
	}
	data, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		log.Fatal(err)
	}
	sig := base64.StdEncoding.EncodeToString(ed25519.Sign(key, data))
	if _, err := update.ParseManifest(data, sig, update.PublicKey); err != nil {
		log.Fatal(err)
	}
	for _, a := range m.Assets {
		if err := update.VerifyFile(filepath.Join(*dir, a.Name), a, update.PublicKey); err != nil {
			log.Fatal(err)
		}
	}
	must(os.WriteFile(filepath.Join(*dir, "latest.json"), data, 0o644))
	must(os.WriteFile(filepath.Join(*dir, "latest.json.sig"), []byte(sig+"\n"), 0o644))
	fmt.Printf("latest.json: %s (%s), %d assets\n", m.Version, m.Channel, len(m.Assets))
}

func build(base, dir, version, tag, repo, notes string, key ed25519.PrivateKey) (*update.Manifest, error) {
	channel := update.Stable
	if update.IsPrerelease(version) {
		channel = update.Beta
	}
	m := &update.Manifest{Version: version, Channel: channel, Date: time.Now().UTC().Format(time.RFC3339), Notes: notes,
		Page: base + "/" + repo + "/releases/tag/" + tag, Assets: map[string]update.Asset{}}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	for _, e := range entries {
		k := update.AssetKeyFor(e.Name())
		if k == "" || e.IsDir() {
			continue
		}
		sum, size, err := hash(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		m.Assets[k] = update.Asset{
			Name: e.Name(), Size: size, SHA256: sum,
			URL: base + "/" + repo + "/releases/download/" + tag + "/" + e.Name(),
			Sig: base64.StdEncoding.EncodeToString(ed25519.Sign(key, update.AssetMessage(e.Name(), sum))),
		}
	}
	if len(m.Assets) == 0 {
		return nil, fmt.Errorf("no release assets in %s", dir)
	}
	return m, nil
}

func hash(path string) (string, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", 0, err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), n, err
}

// notes is the changelog's section for version, without its heading.
func notes(path, version string) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer f.Close()
	var b strings.Builder
	in := false
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "## ") {
			if in {
				break
			}
			in = strings.TrimSpace(strings.TrimPrefix(line, "## ")) == version
			continue
		}
		if in {
			b.WriteString(line + "\n")
		}
	}
	return strings.TrimSpace(b.String())
}

func must(err error) {
	if err != nil {
		log.Fatal(err)
	}
}
