//go:build unix

package core

import (
	"io"
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"time"
)

// WatchdogEnv names the variable that turns Kumo's executable into the
// core's watchdog. The kernel's parent-death signal does not survive the
// exec of a core with file capabilities, and macOS has none, so a small
// process ends the core when Kumo dies without stopping it.
//
// main checks this variable before anything else, so the watchdog never
// starts the GUI.
const WatchdogEnv = "KUMO_WATCHDOG"

// watch starts the watchdog of the core with pid. The watchdog holds the
// read end of a pipe whose write end only Kumo has: when Kumo dies, the
// kernel closes it and the watchdog wakes. The returned func ends the
// watchdog once the core has ended normally, so that it never signals a
// process that later got the same pid.
func watch(pid int) (stop func()) {
	exe, err := os.Executable()
	if err != nil {
		return func() {}
	}
	r, w, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	cmd := exec.Command(exe)
	cmd.Env = append(os.Environ(), WatchdogEnv+"="+strconv.Itoa(pid)+":"+processName(pid))
	cmd.Stdin = r
	if cmd.Start() != nil {
		r.Close()
		w.Close()
		return func() {}
	}
	r.Close()
	go cmd.Wait()
	return func() {
		cmd.Process.Kill()
		w.Close()
	}
}

// RunWatchdog waits until Kumo is gone (its pipe on stdin closes), then
// interrupts the core, and kills it after 2 seconds. It acts only while
// the pid still names the process it was given, and returns early when the
// core ends first.
func RunWatchdog(arg string) {
	runWatchdog(arg, os.Stdin)
}

func runWatchdog(arg string, kumo io.Reader) {
	p, name, _ := cutColon(arg)
	pid, err := strconv.Atoi(p)
	if err != nil || pid <= 1 {
		return
	}
	same := func() bool { return syscall.Kill(pid, 0) == nil && processName(pid) == name }
	gone := make(chan struct{})
	go func() {
		io.Copy(io.Discard, kumo) // returns at EOF: Kumo is gone
		close(gone)
	}()
	for {
		select {
		case <-gone:
			if !same() {
				return
			}
			syscall.Kill(pid, syscall.SIGINT)
			for i := 0; i < 20 && same(); i++ {
				time.Sleep(100 * time.Millisecond)
			}
			if same() {
				syscall.Kill(pid, syscall.SIGKILL)
			}
			return
		case <-time.After(time.Second):
			if !same() {
				return
			}
		}
	}
}

func cutColon(s string) (string, string, bool) {
	for i := range len(s) {
		if s[i] == ':' {
			return s[:i], s[i+1:], true
		}
	}
	return s, "", false
}
