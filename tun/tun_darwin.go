package tun

import (
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"
	"sync"
	"syscall"
)

// Root is the root-owned directory of the helper and its core.
const Root = "/Library/Application Support/Kumo"

func paths() helperPaths { return helperPaths{root: Root, uid: os.Getuid()} }

// Dest is the privileged core.
func Dest() string { return paths().core() }

func plistPath(p helperPaths) string { return "/Library/LaunchDaemons/" + p.label() + ".plist" }

func rootFile(path string, perm os.FileMode) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.Mode().IsRegular() || fi.Mode()&(os.ModeSetuid|os.ModeSetgid) != 0 || fi.Mode().Perm()&^perm != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && st.Uid == 0
}

func ownedDir(path string, uid int) bool {
	fi, err := os.Lstat(path)
	if err != nil || !fi.IsDir() || fi.Mode().Perm()&0o022 != 0 {
		return false
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	return ok && int(st.Uid) == uid
}

func core(src string) (Launch, bool) {
	if os.Geteuid() == 0 {
		return Launch{Binary: src}, true
	}
	p := paths()
	if !ready(p) {
		return Launch{}, false
	}
	exe, err := os.Executable()
	if err != nil {
		return Launch{}, false
	}
	return Launch{Binary: exe, Env: []string{ClientEnv + "=" + Root}}, true
}

// ready checks the helper: the pinned core, the current helper program and
// daemon, root's, and the user's request directory.
func ready(p helperPaths) bool {
	if !rootFile(p.core(), 0o755) || !rootFile(p.helper(), 0o755) || !rootFile(plistPath(p), 0o644) {
		return false
	}
	if script, err := os.ReadFile(p.helper()); err != nil || string(script) != helperScript(p, selfCDHash()) {
		return false
	}
	if exe, err := os.Executable(); err != nil || !rootFile(p.filter(), 0o755) || fileDigest(p.filter()) == "" || fileDigest(p.filter()) != fileDigest(exe) {
		return false
	}
	return ownedDir(p.users(), p.uid) && ownedDir(p.home(), 0) && trusted(p.core())
}

const (
	exitUntrusted = 4
	exitFailed    = 5
)

// installScript is run as root by the administrator prompt. The core is
// copied to a root-owned temporary file and checked there against the
// pinned digests before it is moved into place.
//
// Kumo's own executable is copied the same way to be the controller
// filter, checked against the digest Kumo computed of itself.
func installScript(src string, p helperPaths, gid string) string {
	exe, _ := os.Executable()
	return installScriptFor(src, exe, fileDigest(exe), selfCDHash(), p, gid)
}

func installScriptFor(src, exe, exeDigest, cdhash string, p helperPaths, gid string) string {
	b64 := func(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }
	r, u := shellQuote(p.root), strconv.Itoa(p.uid)
	fail := fmt.Sprintf("exit %d", exitFailed)
	return strings.Join([]string{
		"PATH=/usr/bin:/bin:/usr/sbin:/sbin",
		"R=" + r,
		`install -d -o root -g wheel -m 0755 "$R" "$R/home" "$R/logs" "$R/users" || ` + fail,
		`chown root:wheel "$R" && chmod 0755 "$R" || ` + fail,
		"install -d -o root -g wheel -m 0700 " + shellQuote(p.home()) + " || " + fail,
		"install -d -o root -g wheel -m 0755 " + shellQuote(p.logs()) + " || " + fail,
		"rm -f " + shellQuote(p.users()) + " 2>/dev/null",
		"install -d -o " + u + " -g " + gid + " -m 0700 " + shellQuote(p.users()) + " || " + fail,
		"for f in core.log state; do F=" + shellQuote(p.logs()) + `/$f; rm -f "$F"; : > "$F" && chown ` + u + ` "$F" && chmod 0600 "$F" || ` + fail + "; done",
		`T=$(mktemp "$R/.mihomo.XXXXXX") || ` + fail,
		`cat -- ` + shellQuote(src) + ` > "$T" || { rm -f "$T"; ` + fail + "; }",
		`h=$(shasum -a 256 "$T" | cut -d' ' -f1)`,
		fmt.Sprintf(`case "$h" in %s) ;; *) rm -f "$T"; exit %d;; esac`, hashCase(), exitUntrusted),
		`chown root:wheel "$T" && chmod 0755 "$T" && mv -f "$T" "$R/mihomo" || { rm -f "$T"; ` + fail + "; }",
		`T=$(mktemp "$R/.filter.XXXXXX") || ` + fail,
		`cat -- ` + shellQuote(exe) + ` > "$T" || { rm -f "$T"; ` + fail + "; }",
		fmt.Sprintf(`[ "$(shasum -a 256 "$T" | cut -d' ' -f1)" = %s ] || { rm -f "$T"; exit %d; }`, shellQuote(exeDigest), exitUntrusted),
		`chown root:wheel "$T" && chmod 0755 "$T" && mv -f "$T" "$R/kumo-filter" || { rm -f "$T"; ` + fail + "; }",
		"echo " + b64(helperScript(p, cdhash)) + ` | base64 -D > "$R/.helper" && chown root:wheel "$R/.helper" && chmod 0755 "$R/.helper" && mv -f "$R/.helper" ` + shellQuote(p.helper()) + " || " + fail,
		"P=" + shellQuote(plistPath(p)),
		"echo " + b64(plist(p)) + ` | base64 -D > "$P.new" && chown root:wheel "$P.new" && chmod 0644 "$P.new" && mv -f "$P.new" "$P" || ` + fail,
		"launchctl bootout system/" + p.label() + " 2>/dev/null",
		`launchctl bootstrap system "$P" || ` + fail,
	}, "\n")
}

// appleScript wraps a shell command for osascript's administrator prompt.
func appleScript(shell, prompt string) string {
	q := func(s string) string { return strings.ReplaceAll(strings.ReplaceAll(s, `\`, `\\`), `"`, `\"`) }
	return `do shell script "` + q(shell) + `" with prompt "` + q(prompt) + `" with administrator privileges`
}

func asAdmin(script string) error {
	out, err := exec.Command("/usr/bin/osascript", "-e", appleScript(script, Prompt)).CombinedOutput()
	if err == nil {
		return nil
	}
	msg := string(out)
	switch {
	case strings.Contains(msg, "-128") || strings.Contains(msg, "User canceled"):
		return ErrDenied
	case strings.Contains(msg, fmt.Sprintf("(%d)", exitUntrusted)):
		return ErrUntrusted
	}
	return errors.New(strings.TrimSpace(msg))
}

func install(src string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	p := paths()
	if err := asAdmin(installScript(src, p, u.Gid)); err != nil {
		return err
	}
	if !ready(p) {
		return errors.New("the Enhanced Mode helper was not installed")
	}
	return nil
}

// legacy: Kumo 0.4.3 installed the core setuid root at Root/mihomo.
func legacy() bool {
	fi, err := os.Lstat(Dest())
	return err == nil && fi.Mode()&os.ModeSetuid != 0
}

func removeLegacy() error { return asAdmin("rm -f " + shellQuote(Dest())) }

// LegacyPath is where Kumo 0.4.3 put its setuid core.
func LegacyPath() string { return Dest() }

// fileDigest is the SHA-256 of a file, in hex, or "".
func fileDigest(path string) string {
	d, _ := digest(path)
	return d
}

var cdhashOnce = sync.OnceValue(func() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	out, _ := exec.Command("/usr/bin/codesign", "-dvvv", exe).CombinedOutput()
	return parseCDHash(string(out))
})

// selfCDHash is the code directory hash of Kumo's executable, which the
// helper requires of its client. Release builds are ad-hoc signed, so the
// hash names this exact build; an unsigned build has none, and the helper
// then falls back to the process name check.
func selfCDHash() string { return cdhashOnce() }
