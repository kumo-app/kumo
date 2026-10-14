//go:build unix

package core

import (
	"os"
	"os/exec"
	"strconv"
	"syscall"
	"testing"
	"time"
)

func TestWatchdogEndsCoreWhenKumoGoes(t *testing.T) {
	core := exec.Command("sleep", "30")
	core.Start()
	defer core.Process.Kill()
	pid := core.Process.Pid
	r, w, _ := os.Pipe()
	done := make(chan struct{})
	go func() { runWatchdog(strconv.Itoa(pid)+":"+processName(pid), r); close(done) }()
	time.Sleep(200 * time.Millisecond)
	w.Close() // Kumo died
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("watchdog did not act")
	}
	if err := core.Wait(); err == nil {
		t.Fatal("core still ran normally")
	}
}

func TestWatchdogSparesAnotherProcess(t *testing.T) {
	other := exec.Command("sleep", "30")
	other.Start()
	defer other.Process.Kill()
	r, w, _ := os.Pipe()
	done := make(chan struct{})
	// The pid now names a process other than the core it was given.
	go func() { runWatchdog(strconv.Itoa(other.Process.Pid)+":mihomo", r); close(done) }()
	w.Close()
	<-done
	if err := syscall.Kill(other.Process.Pid, 0); err != nil {
		t.Fatalf("the other process was signalled: %v", err)
	}
}
