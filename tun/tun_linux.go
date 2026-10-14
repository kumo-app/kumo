package tun

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"
)

// Root is the root-owned directory of the copies, one per user.
const Root = "/usr/local/lib/kumo"

// legacyDest is where Kumo 0.4.3 put a copy anyone could run.
const legacyDest = Root + "/mihomo"

// Dest is the privileged copy for the current user.
func Dest() string { return fmt.Sprintf("%s/%d/mihomo", Root, os.Getuid()) }

func core(src string) (Launch, bool) {
	if os.Geteuid() == 0 {
		return Launch{Binary: src}, true
	}
	if ready(Dest()) {
		return Launch{Binary: Dest()}, true
	}
	return Launch{}, false
}

// ready checks the copy: root's, not writable by others, runnable by no
// one but root and us, with capabilities, and a pinned core.
func ready(path string) bool {
	var st unix.Stat_t
	if unix.Lstat(path, &st) != nil || st.Mode&unix.S_IFMT != unix.S_IFREG || st.Uid != 0 || st.Mode&0o027 != 0 {
		return false
	}
	return hasCaps(path) && unix.Access(path, unix.X_OK) == nil && trusted(path)
}

// hasCaps says whether path carries file capabilities.
func hasCaps(path string) bool {
	n, err := unix.Getxattr(path, "security.capability", nil)
	return err == nil && n > 0
}

// privateGroup says whether gid is a group of the user alone (the usual
// user private group), so that it may grant the user access.
func privateGroup(u *user.User, gid string) bool {
	g, err := user.LookupGroupId(gid)
	if err != nil || g.Name != u.Username {
		return false
	}
	f, err := os.Open("/etc/group")
	if err != nil {
		return false
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		parts := strings.Split(sc.Text(), ":")
		if len(parts) == 4 && parts[2] == gid {
			for _, m := range strings.Split(parts[3], ",") {
				if m != "" && m != u.Username {
					return false
				}
			}
		}
	}
	return true
}

// Exit codes of the install script.
const (
	exitNoTool    = 3
	exitUntrusted = 4
	exitFailed    = 5
	exitNoACL     = 6
)

// installScript is what pkexec runs as root. It copies src to a root-owned
// temporary file, checks that copy against the pinned digests, limits it
// to the user (private group, else an ACL), grants the capabilities, and
// only then moves it into place.
func installScript(src string, uid int, gid string, private bool) string {
	d := fmt.Sprintf("%s/%d", Root, uid)
	access := "chown root:" + gid + ` "$T" && chmod 0750 "$T"`
	if !private {
		access = `chown root:root "$T" && chmod 0700 "$T" && { setfacl -m u:` + strconv.Itoa(uid) + `:rx "$T" || { rm -f "$T"; exit ` + strconv.Itoa(exitNoACL) + `; }; }`
	}
	return strings.Join([]string{
		"PATH=/usr/sbin:/sbin:/usr/bin:/bin",
		fmt.Sprintf("for t in setcap sha256sum; do command -v $t >/dev/null || exit %d; done", exitNoTool),
		fmt.Sprintf("install -d -o root -g root -m 0755 %s %s || exit %d", Root, d, exitFailed),
		fmt.Sprintf(`T=$(mktemp %s/.mihomo.XXXXXX) || exit %d`, d, exitFailed),
		fmt.Sprintf(`cat -- %s > "$T" || { rm -f "$T"; exit %d; }`, shellQuote(src), exitFailed),
		`h=$(sha256sum "$T" | cut -d' ' -f1)`,
		fmt.Sprintf(`case "$h" in %s) ;; *) rm -f "$T"; exit %d;; esac`, hashCase(), exitUntrusted),
		access + fmt.Sprintf(` || { rm -f "$T"; exit %d; }`, exitFailed),
		fmt.Sprintf(`setcap cap_net_admin,cap_net_bind_service=+ep "$T" || { rm -f "$T"; exit %d; }`, exitFailed),
		fmt.Sprintf(`mv -f "$T" %s/mihomo || exit %d`, d, exitFailed),
		"rm -f " + legacyDest,
	}, "\n")
}

func pkexec(script string) error {
	path, err := exec.LookPath("pkexec")
	if err != nil {
		return ErrNoTool
	}
	out, err := exec.Command(path, "/bin/sh", "-c", script).CombinedOutput()
	return pkexecError(err, out)
}

// pkexecError reads the outcome. pkexec exits 126 when the dialog is
// dismissed and 127 when authorization fails; the script checks its own
// tools first, so its "not found" never reaches 127.
func pkexecError(err error, out []byte) error {
	var exit *exec.ExitError
	if !errors.As(err, &exit) {
		return err
	}
	switch exit.ExitCode() {
	case 126, 127:
		return ErrDenied
	case exitNoTool:
		return ErrNoTool
	case exitUntrusted:
		return ErrUntrusted
	case exitNoACL:
		return ErrNoACL
	}
	if msg := strings.TrimSpace(string(out)); msg != "" {
		return errors.New(msg)
	}
	return err
}

func install(src string) error {
	u, err := user.Current()
	if err != nil {
		return err
	}
	if err := pkexec(installScript(src, os.Getuid(), u.Gid, privateGroup(u, u.Gid))); err != nil {
		return err
	}
	if _, ok := core(src); !ok {
		return errors.New("the core copy could not be given network rights")
	}
	return nil
}

func legacy() bool {
	fi, err := os.Lstat(legacyDest)
	return err == nil && fi.Mode().IsRegular() && fi.Mode()&0o001 != 0 && hasCaps(legacyDest)
}

func removeLegacy() error { return pkexec("rm -f " + legacyDest) }

// LegacyPath is where Kumo 0.4.3 put its privileged core.
func LegacyPath() string { return legacyDest }
