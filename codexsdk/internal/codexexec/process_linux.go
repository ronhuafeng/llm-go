package codexexec

import (
	"os"
	"os/exec"
	"syscall"
)

// Kill the entire CLI process group on cancellation so an in-flight sandbox
// command cannot continue changing the proposal after the checkpoint is taken.
func configureCancellation(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if err == syscall.ESRCH {
			return os.ErrProcessDone
		}
		return err
	}
}
