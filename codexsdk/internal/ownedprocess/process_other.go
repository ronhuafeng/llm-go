//go:build !linux

package ownedprocess

import (
	"errors"
	"os"
	"os/exec"
	"time"
)

// Other platforms retain direct-child cleanup; no process-tree guarantee.
func Configure(cmd *exec.Cmd) {}
func Reap(cmd *exec.Cmd, grace time.Duration) error {
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
