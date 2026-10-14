package core

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// WatchdogEnv is unused on Windows, where a job object does the work.
const WatchdogEnv = "KUMO_WATCHDOG"

// watch puts the core in a job object that ends it when Kumo's last handle
// to the job closes, which the system does when Kumo dies. The returned
// func closes the job once the core has ended.
func watch(pid int) (stop func()) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return func() {}
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info))); err != nil {
		windows.CloseHandle(job)
		return func() {}
	}
	h, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(pid))
	if err != nil {
		windows.CloseHandle(job)
		return func() {}
	}
	defer windows.CloseHandle(h)
	if err := windows.AssignProcessToJobObject(job, h); err != nil {
		windows.CloseHandle(job)
		return func() {}
	}
	return func() { windows.CloseHandle(job) }
}

// RunWatchdog does nothing on Windows.
func RunWatchdog(string) {}
