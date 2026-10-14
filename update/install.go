package update

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Kind is how Kumo was installed, which decides how it is replaced.
type Kind string

const (
	MacApp      Kind = "mac-app"      // Kumo.app, swapped whole
	WinSetup    Kind = "win-setup"    // installed by the NSIS installer, rerun silently
	WinPortable Kind = "win-portable" // the portable folder, swapped whole
	LinuxTar    Kind = "linux-tar"    // the tar.gz folder, swapped whole
	LinuxDeb    Kind = "linux-deb"    // the .deb, left to the package manager
)

// Install is where and how the running Kumo is installed.
type Install struct {
	Kind   Kind
	Target string // the folder (or Kumo.app) that an update replaces
	Exe    string // the executable, relative to Target
	// Manual explains why this install cannot update itself; the user
	// is sent to the download instead.
	Manual string
}

// Reasons an install is updated by hand.
const (
	ReasonTranslocated = "translocated" // macOS runs the app from a read-only copy
	ReasonReadOnly     = "read-only"    // the folder is not writable
	ReasonPackage      = "package"      // installed by the system's package manager
	ReasonUnknown      = "unknown"
)

// Detect works out the install of the executable exe on goos.
func Detect(exe, goos string) Install {
	exe = filepath.Clean(exe)
	switch goos {
	case "darwin":
		i := strings.LastIndex(exe, ".app/Contents/MacOS/")
		if i < 0 {
			return Install{Manual: ReasonUnknown}
		}
		target := exe[:i+len(".app")]
		in := Install{Kind: MacApp, Target: target, Exe: exe[i+len(".app/"):]}
		if strings.Contains(exe, "/AppTranslocation/") {
			in.Manual = ReasonTranslocated
		}
		return in
	case "windows":
		dir := filepath.Dir(exe)
		in := Install{Kind: WinPortable, Target: dir, Exe: filepath.Base(exe)}
		if _, err := os.Stat(filepath.Join(dir, "Uninstall.exe")); err == nil {
			in.Kind = WinSetup
		}
		return in
	case "linux":
		dir := filepath.Dir(exe)
		if strings.HasPrefix(dir, "/opt/") || strings.HasPrefix(dir, "/usr/") {
			return Install{Kind: LinuxDeb, Target: dir, Exe: filepath.Base(exe), Manual: ReasonPackage}
		}
		return Install{Kind: LinuxTar, Target: dir, Exe: filepath.Base(exe)}
	}
	return Install{Manual: ReasonUnknown}
}

// CheckWritable sets Manual when the folder holding Target cannot take
// the new copy beside it.
func (in *Install) CheckWritable() {
	if in.Manual != "" || in.Kind == WinSetup {
		return
	}
	f, err := os.CreateTemp(filepath.Dir(in.Target), ".kumo-write-test-*")
	if err != nil {
		in.Manual = ReasonReadOnly
		return
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
}

// Plan is what the helper does once Kumo has quit. It is written as JSON
// next to the helper, which finds it through PlanEnv.
type Plan struct {
	Kind      Kind   `json:"kind"`
	PID       int    `json:"pid"`
	Target    string `json:"target"`
	Exe       string `json:"exe"`
	Staged    string `json:"staged,omitempty"`    // the new copy, beside Target
	Installer string `json:"installer,omitempty"` // WinSetup
	Version   string `json:"version"`
	Work      string `json:"work"` // the helper's folder: log, result
	// Wait bounds the wait for the new version to report it started.
	Wait time.Duration `json:"wait"`
}

// Environment variables of the helper and of the Kumo it starts.
const (
	PlanEnv = "KUMO_UPDATE_PLAN" // run as the update helper
	OKEnv   = "KUMO_UPDATE_OK"   // write this file once started
)

// Result is what the helper leaves for the next Kumo to report.
type Result struct {
	Version string `json:"version"`
	OK      bool   `json:"ok"`
	Error   string `json:"error,omitempty"`
	// RolledBack is true when the previous version was put back.
	RolledBack bool `json:"rolledBack,omitempty"`
}

// ResultFile is where the helper leaves its Result in work.
func ResultFile(work string) string { return filepath.Join(work, "result.json") }

// TakeResult reads and removes the helper's result, if any.
func TakeResult(work string) (*Result, bool) {
	p := ResultFile(work)
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, false
	}
	os.Remove(p)
	var r Result
	if json.Unmarshal(data, &r) != nil {
		return nil, false
	}
	return &r, true
}

// stageDir is the folder beside target where the new copy unpacks, on
// the same volume so that swapping is a rename.
func stageDir(target string) string {
	return filepath.Join(filepath.Dir(target), ".kumo-update-"+filepath.Base(target))
}

// Prepare unpacks the verified download for in and returns the plan that
// installs it. Nothing of the running install changes yet.
func Prepare(in Install, download, version, work string) (*Plan, error) {
	if in.Manual != "" {
		return nil, fmt.Errorf("update: this install updates by hand (%s)", in.Manual)
	}
	p := &Plan{Kind: in.Kind, PID: os.Getpid(), Target: in.Target, Exe: in.Exe, Version: version, Work: work, Wait: 90 * time.Second}
	if err := os.MkdirAll(work, 0o700); err != nil {
		return nil, err
	}
	if in.Kind == WinSetup {
		p.Installer = download
		return p, nil
	}
	stage := stageDir(in.Target)
	if err := os.RemoveAll(stage); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(stage, 0o755); err != nil {
		return nil, err
	}
	var err error
	switch in.Kind {
	case MacApp:
		err = unzipMac(download, stage)
		p.Staged = filepath.Join(stage, "Kumo.app")
	case WinPortable:
		err = Unzip(download, stage)
		p.Staged = filepath.Join(stage, "Kumo")
	case LinuxTar:
		p.Staged = filepath.Join(stage, "kumo")
		err = Untar(download, p.Staged)
	default:
		err = fmt.Errorf("update: cannot install %q", in.Kind)
	}
	if err == nil {
		if _, serr := os.Stat(filepath.Join(p.Staged, in.Exe)); serr != nil {
			err = fmt.Errorf("update: the download has no %s", in.Exe)
		}
	}
	if err != nil {
		os.RemoveAll(stage)
		return nil, err
	}
	if in.Kind == MacApp {
		// Our own, verified download: macOS should not hold it back as
		// an unknown file from the internet (the builds are not notarized).
		_ = exec.Command("xattr", "-dr", "com.apple.quarantine", p.Staged).Run()
	}
	return p, nil
}

// unzipMac unpacks with ditto, which keeps the bundle's symlinks,
// extended attributes and code signature intact.
func unzipMac(zipPath, dest string) error {
	if runtime.GOOS != "darwin" {
		return Unzip(zipPath, dest)
	}
	if out, err := exec.Command("ditto", "-x", "-k", zipPath, dest).CombinedOutput(); err != nil {
		return fmt.Errorf("update: ditto: %v: %s", err, out)
	}
	return nil
}

// within joins name to dest, refusing names that leave it.
func within(dest, name string) (string, error) {
	p := filepath.Join(dest, name)
	if p != filepath.Clean(dest) && !strings.HasPrefix(p, filepath.Clean(dest)+string(os.PathSeparator)) {
		return "", fmt.Errorf("update: archive entry %q leaves the folder", name)
	}
	return p, nil
}

// Unzip unpacks a zip into dest.
func Unzip(zipPath, dest string) error {
	r, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer r.Close()
	for _, f := range r.File {
		p, err := within(dest, f.Name)
		if err != nil {
			return err
		}
		mode := f.Mode()
		switch {
		case mode.IsDir():
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case mode&os.ModeSymlink != 0:
			rc, err := f.Open()
			if err != nil {
				return err
			}
			link, err := io.ReadAll(io.LimitReader(rc, 4096))
			rc.Close()
			if err != nil {
				return err
			}
			if err := symlinkWithin(dest, p, string(link)); err != nil {
				return err
			}
		default:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			rc, err := f.Open()
			if err != nil {
				return err
			}
			err = writeFile(p, rc, mode.Perm()|0o600)
			rc.Close()
			if err != nil {
				return err
			}
		}
	}
	return nil
}

// Untar unpacks a .tar.gz into dest.
func Untar(tgz, dest string) error {
	f, err := os.Open(tgz)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	if err := os.MkdirAll(dest, 0o755); err != nil {
		return err
	}
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		p, err := within(dest, h.Name)
		if err != nil {
			return err
		}
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(p, 0o755); err != nil {
				return err
			}
		case tar.TypeSymlink:
			if err := symlinkWithin(dest, p, h.Linkname); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				return err
			}
			if err := writeFile(p, tr, os.FileMode(h.Mode).Perm()|0o600); err != nil {
				return err
			}
		}
	}
}

func symlinkWithin(dest, p, link string) error {
	if filepath.IsAbs(link) {
		return fmt.Errorf("update: absolute symlink %q", link)
	}
	to := filepath.Join(filepath.Dir(p), link)
	if d := filepath.Clean(dest); to != d && !strings.HasPrefix(to, d+string(os.PathSeparator)) {
		return fmt.Errorf("update: symlink %q leaves the folder", link)
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return os.Symlink(link, p)
}

func writeFile(p string, r io.Reader, mode os.FileMode) error {
	out, err := os.OpenFile(p, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, r); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Start copies the running executable into the plan's work folder, writes
// the plan, and starts that copy as the helper, detached, to wait for
// Kumo to quit. The caller quits right after.
func Start(p *Plan) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	helper := filepath.Join(p.Work, "kumo-updater")
	if runtime.GOOS == "windows" {
		helper += ".exe"
	}
	src, err := os.Open(exe)
	if err != nil {
		return err
	}
	err = writeFile(helper, src, 0o700)
	src.Close()
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(p, "", "  ")
	if err != nil {
		return err
	}
	plan := filepath.Join(p.Work, "plan.json")
	if err := os.WriteFile(plan, data, 0o600); err != nil {
		return err
	}
	os.Remove(ResultFile(p.Work))
	cmd := exec.Command(helper)
	cmd.Env = append(os.Environ(), PlanEnv+"="+plan)
	cmd.Dir = p.Work
	detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}

// RunHelper carries out the plan at path once its Kumo has quit. It is
// the whole program when PlanEnv is set.
func RunHelper(path string) int {
	data, err := os.ReadFile(path)
	if err != nil {
		return 2
	}
	var p Plan
	if err := json.Unmarshal(data, &p); err != nil {
		return 2
	}
	lf, _ := os.OpenFile(filepath.Join(p.Work, "update.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if lf != nil {
		defer lf.Close()
	}
	lg := log.New(io.MultiWriter(os.Stderr, orDiscard(lf)), "kumo-update ", log.LstdFlags)
	r := apply(&p, lg)
	out, _ := json.Marshal(r)
	_ = os.WriteFile(ResultFile(p.Work), out, 0o600)
	if r.OK {
		return 0
	}
	return 1
}

func orDiscard(f *os.File) io.Writer {
	if f == nil {
		return io.Discard
	}
	return f
}

// hooks the tests replace.
var (
	launch = func(exe string, env []string) (proc, error) {
		cmd := exec.Command(exe)
		cmd.Env = env
		cmd.Dir = filepath.Dir(exe)
		detach(cmd)
		if err := cmd.Start(); err != nil {
			return nil, err
		}
		done := make(chan struct{})
		go func() { _ = cmd.Wait(); close(done) }()
		return &osProc{cmd, done}, nil
	}
	runInstaller = func(path string) error {
		return exec.Command(path, "/S").Run()
	}
	waitExit = waitPID
)

type proc interface {
	Exited() bool
	Kill()
}

type osProc struct {
	cmd  *exec.Cmd
	done chan struct{}
}

func (o *osProc) Exited() bool {
	select {
	case <-o.done:
		return true
	default:
		return false
	}
}

func (o *osProc) Kill() { _ = o.cmd.Process.Kill() }

func apply(p *Plan, lg *log.Logger) Result {
	r := Result{Version: p.Version}
	lg.Printf("waiting for Kumo (pid %d) to quit", p.PID)
	if !waitExit(p.PID, 2*time.Minute) {
		r.Error = "Kumo did not quit"
		lg.Print(r.Error)
		return r
	}
	exe := filepath.Join(p.Target, p.Exe)
	if p.Kind == WinSetup {
		lg.Printf("running %s /S", p.Installer)
		if err := runInstaller(p.Installer); err != nil {
			r.Error = "installer: " + err.Error()
			lg.Print(r.Error)
			startPlain(exe, lg)
			return r
		}
		if started(exe, p.Wait, lg) {
			r.OK = true
			os.Remove(p.Installer)
		} else {
			r.Error = "the new version did not start"
		}
		return r
	}
	old := p.Target + ".old"
	_ = os.RemoveAll(old)
	if err := os.Rename(p.Target, old); err != nil {
		r.Error = "moving the current version aside: " + err.Error()
		lg.Print(r.Error)
		startPlain(exe, lg)
		return r
	}
	if err := os.Rename(p.Staged, p.Target); err != nil {
		r.Error = "moving the new version in: " + err.Error()
		lg.Print(r.Error)
		r.RolledBack = restore(p, old, lg)
		startPlain(exe, lg)
		return r
	}
	if p.Kind != MacApp {
		carryOver(old, p.Target, lg)
	}
	if started(exe, p.Wait, lg) {
		r.OK = true
		_ = os.RemoveAll(old)
		_ = os.RemoveAll(filepath.Dir(p.Staged))
		lg.Printf("updated to %s", p.Version)
		return r
	}
	r.Error = "the new version did not start"
	lg.Print(r.Error + "; rolling back")
	_ = os.RemoveAll(p.Target + ".failed")
	if err := os.Rename(p.Target, p.Target+".failed"); err != nil {
		lg.Printf("moving the failed version aside: %v", err)
	}
	r.RolledBack = restore(p, old, lg)
	_ = os.RemoveAll(p.Target + ".failed")
	_ = os.RemoveAll(filepath.Dir(p.Staged))
	startPlain(exe, lg)
	return r
}

func restore(p *Plan, old string, lg *log.Logger) bool {
	if err := os.Rename(old, p.Target); err != nil {
		lg.Printf("restoring the previous version: %v", err)
		return false
	}
	lg.Print("restored the previous version")
	return true
}

// carryOver moves into the new folder what the user kept in the old one
// that the new version does not ship.
func carryOver(old, target string, lg *log.Logger) {
	entries, err := os.ReadDir(old)
	if err != nil {
		return
	}
	for _, e := range entries {
		if _, err := os.Lstat(filepath.Join(target, e.Name())); err == nil {
			continue
		}
		if err := os.Rename(filepath.Join(old, e.Name()), filepath.Join(target, e.Name())); err == nil {
			lg.Printf("kept %s", e.Name())
		}
	}
}

// started launches exe and waits for it to write its OK file.
func started(exe string, wait time.Duration, lg *log.Logger) bool {
	ok := filepath.Join(os.TempDir(), fmt.Sprintf("kumo-update-ok-%d", time.Now().UnixNano()))
	defer os.Remove(ok)
	env := append(cleanEnv(), OKEnv+"="+ok)
	pr, err := launch(exe, env)
	if err != nil {
		lg.Printf("starting %s: %v", exe, err)
		return false
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(ok); err == nil {
			return true
		}
		if pr.Exited() {
			lg.Print("the new version exited before it started")
			return false
		}
		time.Sleep(250 * time.Millisecond)
	}
	lg.Print("the new version did not report in time")
	pr.Kill()
	return false
}

func startPlain(exe string, lg *log.Logger) {
	if _, err := launch(exe, cleanEnv()); err != nil {
		lg.Printf("starting %s: %v", exe, err)
	}
}

func cleanEnv() []string {
	var env []string
	for _, kv := range os.Environ() {
		if !strings.HasPrefix(kv, PlanEnv+"=") && !strings.HasPrefix(kv, OKEnv+"=") {
			env = append(env, kv)
		}
	}
	return env
}

// ReportStarted writes the OK file the helper waits for, if any.
func ReportStarted() {
	if p := os.Getenv(OKEnv); p != "" {
		_ = os.WriteFile(p, []byte("ok"), 0o600)
		os.Unsetenv(OKEnv)
	}
}
