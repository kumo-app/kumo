// Package tun prepares the core of Kumo's Enhanced Mode (mihomo's TUN),
// which needs rights an ordinary process lacks, after one authorization by
// the user:
//
//   - Linux: pkexec copies the core into /usr/local/lib/kumo/<uid>/, which
//     only that user may run, and gives it cap_net_admin and
//     cap_net_bind_service. It runs as the user, with those rights.
//   - macOS: an administrator prompt installs a launchd daemon for the
//     user (app.kumo.tun.<uid>) with a root-owned copy of the core. The
//     daemon runs the core as root, in a root-owned home directory, with
//     the configuration Kumo hands over; Kumo runs a small client (its own
//     executable, see RunClient) in the core's place.
//   - Windows: no copy; Kumo itself must run as administrator, and mihomo
//     brings its own wintun driver.
//
// Whatever file it starts from, a core gets these rights only when its
// SHA-256 is one Kumo pinned at build time (package corepin). The check is
// made by root on a root-owned copy, so the file cannot change between
// check and use; a core found on PATH is never privileged.
package tun

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"kumo/corepin"
)

var (
	// ErrDenied means the user cancelled or refused the authorization.
	ErrDenied = errors.New("authorization denied")
	// ErrNeedsAdmin means Kumo must be restarted as administrator.
	ErrNeedsAdmin = errors.New("run Kumo as administrator to use Enhanced Mode")
	// ErrNoTool means the system lacks pkexec, setcap or sha256sum.
	ErrNoTool = errors.New("pkexec, setcap and sha256sum are needed for Enhanced Mode")
	// ErrUntrusted means the core is not one this build of Kumo ships.
	ErrUntrusted = errors.New("the core does not match the one Kumo ships")
	// ErrNoACL means the copy could not be limited to the user (setfacl).
	ErrNoACL = errors.New("setfacl is needed to limit the core to your account")
)

// ClientEnv names the variable that turns Kumo's executable into the
// client of the macOS Enhanced Mode helper (see RunClient); its value is
// the helper's root directory.
const ClientEnv = "KUMO_TUN_CLIENT"

// Prompt is the text of the macOS administrator prompt; Kumo sets it in
// the user's language.
var Prompt = "Kumo is installing its Enhanced Mode helper and a root-owned copy of the mihomo core."

// Launch is how to run the core in Enhanced Mode: Binary with the usual
// core arguments, Env added to the environment.
type Launch struct {
	Binary string
	Env    []string
}

// Core says how to run the core in Enhanced Mode, false while Install is
// needed first. src is the bundled core.
func Core(src string) (Launch, bool) { return core(src) }

// Install sets up Enhanced Mode for src, asking the user once. It blocks
// until the user answers.
func Install(src string) error {
	if !trusted(src) {
		return ErrUntrusted
	}
	return install(src)
}

// Legacy says whether an earlier Kumo (0.4.3) left a privileged core that
// anyone may run: a setuid copy on macOS, one with capabilities on Linux.
func Legacy() bool { return legacy() }

// RemoveLegacy deletes that copy, asking for authorization.
func RemoveLegacy() error { return removeLegacy() }

// trusted says whether path holds a core Kumo pinned for this system.
func trusted(path string) bool {
	sum, err := cachedDigest(path)
	return err == nil && slices.Contains(corepin.Trusted(runtime.GOOS), sum)
}

// Digests are cached by file identity and modification, so that the
// ~100 MB core is not read on every start.
type fileKey struct {
	path  string
	size  int64
	mtime time.Time
	sys   string
}

var (
	digestMu    sync.Mutex
	digestCache = map[fileKey]string{}
)

func cachedDigest(path string) (string, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	key := fileKey{path, fi.Size(), fi.ModTime(), identity(fi)}
	digestMu.Lock()
	sum, ok := digestCache[key]
	digestMu.Unlock()
	if ok {
		return sum, nil
	}
	if sum, err = digest(path); err != nil {
		return "", err
	}
	digestMu.Lock()
	digestCache[key] = sum
	digestMu.Unlock()
	return sum, nil
}

func digest(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// shellQuote quotes s for a POSIX shell.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// hashCase is a shell case pattern of the trusted digests.
func hashCase() string {
	list := corepin.Trusted(runtime.GOOS)
	if len(list) == 0 {
		return "none-trusted"
	}
	return strings.Join(list, "|")
}
