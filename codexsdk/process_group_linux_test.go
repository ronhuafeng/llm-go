package codexsdk

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// A launcher can join its native process without joining the native process's
// background work. Neither that fast exit nor the forced path permits work to
// continue using the caller's temporary directory after Close succeeds.
func TestCloseContainsInheritedBackgroundProcess(t *testing.T) {
	for _, mode := range []string{"normal-exit", "forced-exit"} {
		t.Run(mode, func(t *testing.T) {
			root := t.TempDir()
			c, err := New(ClientOptions{CWD: root, Command: []string{os.Args[0], "-test.run=^TestProcessGroupHelper$"}, Env: append(os.Environ(), "CODEXSDK_GROUP_MODE="+mode, "CODEXSDK_GROUP_ROOT="+root, "GORACE=atexit_sleep_ms=0")})
			if err != nil {
				t.Fatal(err)
			}
			pid := waitFixturePID(t, filepath.Join(root, "background.pid"))
			stopped := false
			t.Cleanup(func() {
				if !stopped {
					_ = syscall.Kill(pid, syscall.SIGKILL)
				}
				_ = c.Close()
			})
			if !groupFixtureRunning(pid) {
				t.Fatal("background process was not alive before close")
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			result, err := c.CloseContext(ctx)
			if err != nil || !result.ProcessReaped {
				t.Fatalf("close: %+v, %v", result, err)
			}
			if groupFixtureRunning(pid) {
				t.Fatal("background process can still write after successful close")
			}
			stopped = true
		})
	}
}

// Linux reaps the direct child through cmd.Wait. An orphaned descendant may be
// a zombie until its external reaper runs; zombies cannot perform filesystem IO.
func groupFixtureRunning(pid int) bool {
	raw, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if os.IsNotExist(err) {
		return false
	}
	if err != nil {
		return true
	}
	tail := strings.LastIndex(string(raw), ") ")
	if tail < 0 {
		return true
	}
	state := raw[tail+2]
	return state != 'Z' && state != 'X'
}

func TestProcessGroupHelper(t *testing.T) {
	mode := os.Getenv("CODEXSDK_GROUP_MODE")
	if mode == "" {
		return
	}
	root := os.Getenv("CODEXSDK_GROUP_ROOT")
	if mode == "background" {
		signal.Ignore(os.Interrupt)
		if os.WriteFile(filepath.Join(root, "background.pid"), []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(2)
		}
		for {
			if os.WriteFile(filepath.Join(root, "background-write"), []byte("owned"), 0600) != nil {
				os.Exit(3)
			}
			time.Sleep(time.Millisecond)
		}
	}
	background := exec.Command(os.Args[0], "-test.run=^TestProcessGroupHelper$")
	background.Env = append(os.Environ(), "CODEXSDK_GROUP_MODE=background")
	if background.Start() != nil {
		os.Exit(2)
	}
	interrupted := make(chan os.Signal, 1)
	signal.Notify(interrupted, os.Interrupt)
	go func() {
		<-interrupted
		if mode == "forced-exit" {
			_ = background.Process.Signal(os.Interrupt)
			_ = background.Wait()
		}
		os.Exit(0)
	}()
	for {
		if _, err := os.Stat(filepath.Join(root, "background.pid")); err == nil {
			break
		}
		time.Sleep(time.Millisecond)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var request map[string]any
		if json.Unmarshal(scanner.Bytes(), &request) != nil {
			os.Exit(2)
		}
		if request["method"] == "initialize" {
			data, _ := json.Marshal(map[string]any{"id": request["id"], "result": map[string]any{"codexHome": root, "platformFamily": "fixture", "platformOs": "linux", "userAgent": "process-group-fixture"}})
			fmt.Println(string(data))
		}
	}
	// EOF doesn't end native/background ownership; wait for explicit shutdown.
	select {}
}
