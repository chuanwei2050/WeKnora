//go:build windows

package sandbox

import (
	"fmt"
	"os/exec"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

func configureProcessCleanup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_BREAKAWAY_FROM_JOB}
}

func startProcess(cmd *exec.Cmd) (func(), func(), error) {
	configureProcessCleanup(cmd)
	if err := cmd.Start(); err != nil {
		// Some hosts disallow CREATE_BREAKAWAY_FROM_JOB. Retry without it;
		// the fallback still gets best-effort task-tree cleanup below.
		cmd.SysProcAttr = nil
		if retryErr := cmd.Start(); retryErr != nil {
			return func() {}, func() {}, retryErr
		}
	}

	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		killProcessTree(cmd)
		return func() {}, func() {}, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)),
		uint32(unsafe.Sizeof(info)),
	); err != nil {
		_ = windows.CloseHandle(job)
		killProcessTree(cmd)
		return func() {}, func() {}, err
	}
	processHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE|windows.PROCESS_QUERY_INFORMATION, false, uint32(cmd.Process.Pid))
	if err != nil {
		_ = windows.CloseHandle(job)
		return func() { killProcessTree(cmd) }, func() {}, nil
	}
	defer windows.CloseHandle(processHandle)
	if err := windows.AssignProcessToJobObject(job, processHandle); err != nil {
		_ = windows.CloseHandle(job)
		return func() { killProcessTree(cmd) }, func() {}, nil
	}

	stop := func() {
		_ = windows.TerminateJobObject(job, 1)
		killProcessTree(cmd)
	}
	cleanup := func() { _ = windows.CloseHandle(job) }
	return stop, cleanup, nil
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = exec.Command("taskkill", "/PID", fmt.Sprint(cmd.Process.Pid), "/T", "/F").Run()
		_ = cmd.Process.Kill()
	}
}
