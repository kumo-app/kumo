package core

import (
	"os/exec"
	"syscall"
)

// hide has the kernel end the core when Kumo dies, so that a crash never
// leaves it holding the ports.
func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGTERM}
}
