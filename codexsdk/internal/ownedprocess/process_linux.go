package ownedprocess

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// A dedicated group bounds termination to the command and children that remain
// in its group. Descendants that deliberately detach are outside this ownership.
func Configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}

func killOwnedGroup(cmd *exec.Cmd) error {
	err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	if err == nil || err == syscall.ESRCH {
		return nil
	}
	return fmt.Errorf("codexsdk: terminate owned process group: %w", err)
}

// Observe exit without reaping so the leader's PID cannot be reused before the
// final group signal. cmd.Wait remains the sole owner of direct-child reaping.
func waitOwnedExit(cmd *exec.Cmd) error {
	var info [16]uint64 // Linux siginfo_t is 128 bytes; no fields need decoding.
	for {
		_, _, errno := syscall.Syscall6(syscall.SYS_WAITID, 1, uintptr(cmd.Process.Pid), uintptr(unsafe.Pointer(&info[0])), syscall.WEXITED|syscall.WNOWAIT, 0, 0)
		if errno == syscall.EINTR {
			continue
		}
		if errno != 0 {
			return fmt.Errorf("codexsdk: observe owned process exit: %w", errno)
		}
		return nil
	}
}

func Reap(cmd *exec.Cmd, grace time.Duration) error {
	exited := make(chan error, 1)
	go func() { exited <- waitOwnedExit(cmd) }()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	var cleanupErr, exitErr error
	select {
	case exitErr = <-exited:
	case <-timer.C:
		cleanupErr = killOwnedGroup(cmd)
		if cleanupErr != nil {
			// Preserve direct-child cleanup even if containment failed.
			cleanupErr = errors.Join(cleanupErr, cmd.Process.Kill())
		}
		exitErr = <-exited
	}
	// If another actor reaped the child, its numeric group identity is no longer
	// ours to signal. Report that failure instead of signaling a potentially reused ID.
	if exitErr == nil {
		cleanupErr = errors.Join(cleanupErr, killOwnedGroup(cmd))
		if cleanupErr == nil {
			cleanupErr = awaitOwnedGroupStopped(cmd.Process.Pid, grace)
		}
	} else if !errors.Is(exitErr, syscall.ECHILD) {
		cleanupErr = errors.Join(cleanupErr, cmd.Process.Kill())
	}
	waitErr := cmd.Wait()
	var exitStatus *exec.ExitError
	if errors.As(waitErr, &exitStatus) {
		waitErr = nil // A signaled child is expected during owned shutdown.
	}
	return errors.Join(cleanupErr, exitErr, waitErr)
}

// Signal delivery is not quiescence: a member may still be finishing a syscall.
// The unreaped leader reserves pgid throughout this observation. No process
// outside that group is signaled, and detached descendants are not claimed.
func awaitOwnedGroupStopped(pgid int, grace time.Duration) error {
	timer := time.NewTimer(grace)
	defer timer.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		stopped, err := ownedGroupStopped(pgid)
		if err != nil {
			return fmt.Errorf("codexsdk: observe owned process group: %w", err)
		}
		if stopped {
			return nil
		}
		select {
		case <-timer.C:
			return errors.New("codexsdk: owned process group did not stop")
		case <-tick.C:
		}
	}
}

func ownedGroupStopped(pgid int) (bool, error) {
	processes, err := os.ReadDir("/proc")
	if err != nil {
		return false, err
	}
	for _, process := range processes {
		if _, err := strconv.Atoi(process.Name()); err != nil {
			continue
		}
		base := filepath.Join("/proc", process.Name())
		data, err := os.ReadFile(filepath.Join(base, "stat"))
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
			continue
		}
		if err != nil {
			return false, err
		}
		_, group, err := ownedProcessStat(data)
		if err != nil {
			return false, err
		}
		if group != pgid {
			continue
		}
		// A zombie group leader can retain live threads. Check every task instead
		// of assuming the process leader's state describes them all.
		tasks, err := os.ReadDir(filepath.Join(base, "task"))
		if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) || (err == nil && len(tasks) == 0) {
			// A missing task view for an extant leader is not proof that its
			// remaining threads have stopped. Only disappearance is conclusive.
			_, statErr := os.Stat(filepath.Join(base, "stat"))
			if os.IsNotExist(statErr) || errors.Is(statErr, syscall.ESRCH) {
				continue
			}
			if statErr != nil {
				return false, statErr
			}
			return false, errors.New("owned process task state unavailable")
		}
		if err != nil {
			return false, err
		}
		for _, task := range tasks {
			data, err := os.ReadFile(filepath.Join(base, "task", task.Name(), "stat"))
			if os.IsNotExist(err) || errors.Is(err, syscall.ESRCH) {
				continue
			}
			if err != nil {
				return false, err
			}
			state, group, err := ownedProcessStat(data)
			if err != nil {
				return false, err
			}
			if group == pgid && state != "Z" && state != "X" {
				return false, nil
			}
		}
	}
	return true, nil
}

func ownedProcessStat(data []byte) (string, int, error) {
	end := strings.LastIndex(string(data), ") ")
	if end < 0 {
		return "", 0, errors.New("invalid process stat")
	}
	fields := strings.Fields(string(data[end+2:]))
	if len(fields) < 3 {
		return "", 0, errors.New("incomplete process stat")
	}
	group, err := strconv.Atoi(fields[2])
	if err != nil {
		return "", 0, errors.New("invalid process group")
	}
	return fields[0], group, nil
}
