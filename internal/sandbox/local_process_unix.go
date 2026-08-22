//go:build !windows

package sandbox

import (
	"os/exec"
	"syscall"
)

func configureProcessCleanup(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func startProcess(cmd *exec.Cmd) (func(), func(), error) {
	configureProcessCleanup(cmd)
	if err := cmd.Start(); err != nil {
		return func() {}, func() {}, err
	}
	stop := func() { killProcessTree(cmd) }
	return stop, func() {}, nil
}

func killProcessTree(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}
