package core

import (
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"golang.org/x/sys/windows"
)

// hide runs the core without a console window.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NO_WINDOW}
}

// terminate ends the core. A console-less process gets no Ctrl+C on
// Windows, so it is killed.
func terminate(cmd *exec.Cmd) { _ = cmd.Process.Kill() }

// processName returns the image name of pid from tasklist, or "".
func processName(pid int) string {
	cmd := exec.Command("tasklist", "/NH", "/FO", "CSV", "/FI", "PID eq "+strconv.Itoa(pid))
	hide(cmd)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	fields := strings.Split(strings.TrimSpace(string(out)), ",")
	return strings.Trim(fields[0], `"`)
}
