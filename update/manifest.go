// Package update finds, verifies and installs new versions of Kumo.
//
// Every release publishes latest.json, a manifest of its assets, and
// latest.json.sig, an Ed25519 signature of the manifest's bytes. Each
// asset in the manifest also carries its own signature over its name and
// SHA-256 (see AssetMessage), so a downloaded file is accepted only when
// its digest matches and that digest was signed by the release key.
package update

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
)

// PublicKey verifies the manifests and assets; its private half is the
// KUMO_UPDATE_KEY secret of the release workflow.
const PublicKey = "fueOXGGaRfbGXR2P2Vb/VyAUqBwmB4P0iWpHoKrvJGc="

// Channels of updates.
const (
	Stable = "stable"
	Beta   = "beta"
)

// Manifest describes one release.
type Manifest struct {
	Version string           `json:"version"`
	Channel string           `json:"channel"`
	Date    string           `json:"date"`
	Notes   string           `json:"notes"`
	Page    string           `json:"page"` // the release's web page
	Assets  map[string]Asset `json:"assets"`
}

// Asset is one downloadable file, by platform key (see Key).
type Asset struct {
	Name   string `json:"name"`
	URL    string `json:"url"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Sig    string `json:"sig"` // base64 Ed25519 over AssetMessage
}

// AssetMessage is what an asset's signature signs.
func AssetMessage(name, sha string) []byte {
	return []byte("kumo-update-v1\n" + name + "\n" + strings.ToLower(sha) + "\n")
}

func publicKey(b64 string) (ed25519.PublicKey, error) {
	k, err := base64.StdEncoding.DecodeString(b64)
	if err != nil || len(k) != ed25519.PublicKeySize {
		return nil, errors.New("update: bad public key")
	}
	return k, nil
}

func verify(pub ed25519.PublicKey, msg []byte, sigB64 string) bool {
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(sigB64))
	return err == nil && len(sig) == ed25519.SignatureSize && ed25519.Verify(pub, msg, sig)
}

// ParseManifest checks sig against data with the public key pub (base64)
// and then decodes it.
func ParseManifest(data []byte, sig, pub string) (*Manifest, error) {
	k, err := publicKey(pub)
	if err != nil {
		return nil, err
	}
	if !verify(k, data, sig) {
		return nil, errors.New("update: the manifest's signature does not verify")
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("update: manifest: %w", err)
	}
	if _, err := ParseVersion(m.Version); err != nil {
		return nil, err
	}
	return &m, nil
}

// VerifyFile checks that the file at path is asset a: its size, its
// SHA-256, and the signature of that digest.
func VerifyFile(path string, a Asset, pub string) error {
	k, err := publicKey(pub)
	if err != nil {
		return err
	}
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		return err
	}
	if a.Size > 0 && n != a.Size {
		return fmt.Errorf("update: %s is %d bytes, expected %d", a.Name, n, a.Size)
	}
	sum := hex.EncodeToString(h.Sum(nil))
	if !strings.EqualFold(sum, a.SHA256) {
		return fmt.Errorf("update: %s has SHA-256 %s, expected %s", a.Name, sum, a.SHA256)
	}
	if !verify(k, AssetMessage(a.Name, sum), a.Sig) {
		return fmt.Errorf("update: the signature of %s does not verify", a.Name)
	}
	return nil
}

// Key names the asset for a platform and kind of install, e.g.
// "darwin-universal-app", "windows-amd64-setup", "linux-arm64-tar".
func Key(goos, goarch string, kind Kind) string {
	switch kind {
	case MacApp:
		return "darwin-universal-app"
	case WinSetup:
		return "windows-" + goarch + "-setup"
	case WinPortable:
		return "windows-" + goarch + "-portable"
	case LinuxTar:
		return "linux-" + goarch + "-tar"
	case LinuxDeb:
		return "linux-" + goarch + "-deb"
	}
	return goos + "-" + goarch
}

// AssetKeyFor maps a release file name to its key, "" for files the
// updater does not use. The release workflow uses it to write manifests.
func AssetKeyFor(name string) string {
	switch {
	case strings.HasSuffix(name, "-macos-universal.app.zip"):
		return "darwin-universal-app"
	case strings.HasPrefix(name, "Kumo-Setup-") && strings.HasSuffix(name, "-amd64.exe"):
		return "windows-amd64-setup"
	case strings.HasSuffix(name, "-windows-amd64-portable.zip"):
		return "windows-amd64-portable"
	case strings.HasPrefix(name, "kumo-") && strings.HasSuffix(name, "-linux-amd64.tar.gz"):
		return "linux-amd64-tar"
	case strings.HasPrefix(name, "kumo-") && strings.HasSuffix(name, "-linux-arm64.tar.gz"):
		return "linux-arm64-tar"
	case strings.HasPrefix(name, "kumo_") && strings.HasSuffix(name, "_amd64.deb"):
		return "linux-amd64-deb"
	case strings.HasPrefix(name, "kumo_") && strings.HasSuffix(name, "_arm64.deb"):
		return "linux-arm64-deb"
	}
	return ""
}
