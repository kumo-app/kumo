//go:build unix

package core

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
)

func terminate(cmd *exec.Cmd) { cmd.Process.Signal(os.Interrupt) }

// processName is the command name of a process, "" when there is none.
func processName(pid int) string {
	if data, err := os.ReadFile("/proc/" + strconv.Itoa(pid) + "/comm"); err == nil {
		return strings.TrimSpace(string(data))
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "comm=").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}
