package update

import (
	"os/exec"
	"syscall"
	"time"
)

const (
	detachedProcess       = 0x00000008
	createNewProcessGroup = 0x00000200
	synchronize           = 0x00100000
)

func detach(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: detachedProcess | createNewProcessGroup}
}

// waitPID waits for process pid to end, at most d.
func waitPID(pid int, d time.Duration) bool {
	h, err := syscall.OpenProcess(synchronize, false, uint32(pid))
	if err != nil {
		return true // gone already
	}
	defer syscall.CloseHandle(h)
	ev, err := syscall.WaitForSingleObject(h, uint32(d/time.Millisecond))
	return err == nil && ev == syscall.WAIT_OBJECT_0
}
