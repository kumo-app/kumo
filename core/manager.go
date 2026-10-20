// Package core runs the bundled mihomo core: it writes the configuration
// the core runs with, starts and stops the process, and keeps the
// profiles. It imports nothing of the UI.
package core

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// State is where the core process is.
type State int

const (
	Stopped State = iota
	Running
	Failed
)

func (s State) String() string {
	return [...]string{"Stopped", "Running", "Failed"}[s]
}

// Manager runs one mihomo process with Dir as its home directory.
type Manager struct {
	Binary string
	// Env is added to the environment of Binary (the Enhanced Mode client).
	Env []string
	Dir string
	// Watchdog starts a watchdog process (Kumo's executable, see
	// RunWatchdog) that ends the core if Kumo dies without stopping it.
	Watchdog bool
	// BeforeStop, when set, runs before the core is asked to end: on
	// Windows, where the core can only be killed, it lets the core take
	// its TUN routes down first.
	BeforeStop func()

	// startMu serializes Start and Stop, so that two starts never leave
	// two cores running.
	startMu  sync.Mutex
	mu       sync.Mutex
	cmd      *exec.Cmd
	done     chan struct{}
	state    State
	err      string
	onChange func()
}

// FindBinary looks for the core among the app's resources: where mygo
// installs it in a package, then in the platform directory of the project
// under `go run`, then on PATH. Only a bundled core (see FindBundled) is
// ever given Enhanced Mode's rights.
func FindBinary(resources string) string {
	if p := FindBundled(resources); p != "" {
		return p
	}
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	return ""
}

// FindBundled is the core among the app's resources, or "".
func FindBundled(resources string) string {
	name := "mihomo"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	for _, p := range []string{
		filepath.Join(resources, "core", name),
		filepath.Join(resources, runtime.GOOS+"-"+runtime.GOARCH, "core", name),
	} {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

// Use makes the next Start (and Test) run binary with env added, as the
// privileged core of Enhanced Mode. It waits for a Start in progress.
func (m *Manager) Use(binary string, env ...string) {
	m.startMu.Lock()
	m.Binary, m.Env = binary, env
	m.startMu.Unlock()
}

func (m *Manager) command(ctx context.Context, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, m.Binary, args...)
	if len(m.Env) > 0 {
		cmd.Env = append(os.Environ(), m.Env...)
	}
	hide(cmd)
	return cmd
}

// OnChange sets what is called, from any goroutine, when the state changes.
func (m *Manager) OnChange(fn func()) { m.onChange = fn }

func (m *Manager) changed() {
	if m.onChange != nil {
		m.onChange()
	}
}

// Status returns the state and the error of the last failure.
func (m *Manager) Status() (State, string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.state, m.err
}

// LogPath is the core's log file.
func (m *Manager) LogPath() string { return filepath.Join(m.Dir, "core.log") }

// Version runs the binary for its version line.
func (m *Manager) Version() string {
	if m.Binary == "" {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	m.startMu.Lock()
	cmd := m.command(ctx, "-v")
	m.startMu.Unlock()
	out, _ := cmd.Output()
	line, _, _ := strings.Cut(string(out), "\n")
	return strings.TrimSpace(line)
}

// Test checks a configuration file with the core, returning its complaint.
func (m *Manager) Test(config string) error {
	if m.Binary == "" {
		return errors.New("no mihomo binary")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := m.command(ctx, "-t", "-d", m.Dir, "-f", config)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s", lastLines(out, 3))
	}
	return nil
}

// pidPath records the running core, to reap it after a crash of Kumo.
func (m *Manager) pidPath() string { return filepath.Join(m.Dir, "mihomo.pid") }

// ReapOrphan ends a core that a previous Kumo left running, as named in
// the pid file, when that process is still a mihomo.
func (m *Manager) ReapOrphan() {
	data, err := os.ReadFile(m.pidPath())
	if err != nil {
		return
	}
	defer os.Remove(m.pidPath())
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 || pid == os.Getpid() {
		return
	}
	if !strings.Contains(strings.ToLower(processName(pid)), "mihomo") {
		return
	}
	if p, err := os.FindProcess(pid); err == nil {
		p.Kill()
		time.Sleep(300 * time.Millisecond)
	}
}

// Start runs the core with config, after stopping any it runs already.
func (m *Manager) Start(config string) error {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.stop()
	if m.Binary == "" {
		m.fail("the mihomo core is not bundled with this build")
		return errors.New(m.err)
	}
	if err := os.MkdirAll(m.Dir, 0o700); err != nil {
		m.fail(err.Error())
		return err
	}
	// Through a pipe, so that the log is capped (LogLimit): a file handed
	// to the core would grow for as long as it runs.
	logf, err := openCapped(m.LogPath(), LogLimit)
	if err != nil {
		m.fail(err.Error())
		return err
	}
	cmd := m.command(context.Background(), "-d", m.Dir, "-f", config)
	cmd.Stdout, cmd.Stderr = logf, logf
	if err := cmd.Start(); err != nil {
		logf.Close()
		m.fail(err.Error())
		return err
	}
	// Without its pid file, a core left by a crash could not be reaped.
	if err := WriteFileAtomic(m.pidPath(), []byte(strconv.Itoa(cmd.Process.Pid)), 0o600); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		logf.Close()
		m.fail("could not record the core's process: " + err.Error())
		return err
	}
	unwatch := func() {}
	if m.Watchdog {
		unwatch = watch(cmd.Process.Pid)
	}
	done := make(chan struct{})
	m.mu.Lock()
	m.cmd, m.done, m.state, m.err = cmd, done, Running, ""
	m.mu.Unlock()
	m.changed()
	go func() {
		err := cmd.Wait()
		unwatch()
		logf.Close()
		close(done)
		m.mu.Lock()
		if m.cmd == cmd {
			m.cmd = nil
			if m.state == Running {
				// It ended without Stop.
				m.state = Failed
				tail, _ := os.ReadFile(m.LogPath())
				m.err = fmt.Sprintf("core exited (%v): %s", err, lastLines(tail, 2))
			}
		}
		m.mu.Unlock()
		m.changed()
	}()
	return nil
}

func (m *Manager) fail(msg string) {
	m.mu.Lock()
	m.state, m.err = Failed, msg
	m.mu.Unlock()
	m.changed()
}

// Stop ends the core, asking first, then killing it after 2 seconds.
func (m *Manager) Stop() {
	m.startMu.Lock()
	defer m.startMu.Unlock()
	m.stop()
}

func (m *Manager) stop() {
	m.mu.Lock()
	cmd, done := m.cmd, m.done
	m.cmd = nil
	if m.state == Running {
		m.state = Stopped
	}
	m.mu.Unlock()
	if cmd == nil {
		return
	}
	if m.BeforeStop != nil {
		m.BeforeStop()
	}
	terminate(cmd)
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		cmd.Process.Kill()
		select {
		case <-done:
		case <-time.After(time.Second):
		}
	}
	os.Remove(m.pidPath())
	m.changed()
}

func lastLines(b []byte, n int) string {
	lines := strings.Split(strings.TrimSpace(string(bytes.ReplaceAll(b, []byte("\r"), nil))), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, " | ")
}
