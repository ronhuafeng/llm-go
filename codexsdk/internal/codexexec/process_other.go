//go:build !linux

package codexexec

import "os/exec"

// The required repair runner is Linux. Other platforms retain CommandContext's
// process cancellation and bounded WaitDelay, without a live-runtime guarantee.
func configureCancellation(cmd *exec.Cmd) {}
