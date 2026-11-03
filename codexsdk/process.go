package codexsdk

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

// Other platforms retain direct-child cleanup; no process-tree guarantee.
var configureOwnedProcess = func(cmd *exec.Cmd) {}
var reapOwnedProcess = reapDirectProcess

func reapDirectProcess(cmd *exec.Cmd, grace time.Duration) error {
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		err := cmd.Process.Kill()
		<-done
		if errors.Is(err, os.ErrProcessDone) {
			return nil
		}
		return err
	}
}
