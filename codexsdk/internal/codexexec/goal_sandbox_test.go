package codexexec

import (
	"context"
	"fmt"
	"github.com/pelletier/go-toml/v2"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestGoalSandboxLayout(t *testing.T) {
	e := goalFixture(t, "isolation")
	feedback := t.TempDir()
	e.config.GoalSandbox.ReadOnlyDirs = []string{feedback}
	cmd, err := e.goalSandboxCommand(e.command(context.Background(), "app-server"))
	if err != nil {
		t.Fatal(err)
	}
	args := strings.Join(cmd.Args, "\n")
	for _, want := range []string{"--unshare-all\n--share-net", "--tmpfs\n/\n", "--proc\n/proc", "--tmpfs\n/tmp", "--tmpfs\n/run", "--ro-bind\n" + feedback + "\n" + feedback, "--ro-bind\n" + filepath.Join(e.config.Workspace, ".git")} {
		if !strings.Contains(args, want) {
			t.Fatalf("missing isolation component %q", want)
		}
	}
	for _, forbidden := range []string{"--ro-bind\n/\n/\n", "--ro-bind\n/etc\n", "--ro-bind\n/opt\n", "--ro-bind\n/home\n"} {
		if strings.Contains(args, forbidden) {
			t.Fatalf("broad host exposure: %q", forbidden)
		}
	}
	if len(cmd.ExtraFiles) != 0 {
		t.Fatal("inherited extra descriptors")
	}
	env := strings.Join(cmd.Env, "\n")
	if !strings.Contains(env, "HOME="+e.home+"\n") || !strings.Contains(env, "TMPDIR=/tmp") {
		t.Fatal("home/temp not private")
	}
	for _, path := range []string{"/", "/opt", "/etc", "/tmp", filepath.Dir(e.config.Workspace), e.config.Workspace, e.home} {
		if e.validGoalInput(path) {
			t.Fatalf("unsafe input accepted: %s", path)
		}
	}
	e.config.GoalSandbox = nil
	if _, err = e.goalSandboxCommand(e.command(context.Background(), "app-server")); err == nil {
		t.Fatal("goal isolation silently disabled")
	}
}

// TestNativeGoalSandboxBoundary is a required CI source-boundary proof. It uses
// real bubblewrap and the installed Codex native sandbox, but no model/provider
// request: provider connectivity is a local HTTP positive control. Ordinary unit
// runs skip it; explicitly selected runs fail if the native boundary is absent.
func TestNativeGoalSandboxBoundary(t *testing.T) {
	if os.Getenv("LLMGO_NATIVE_GOAL_SANDBOX") != "1" {
		t.Skip("explicit native goal boundary suite only")
	}
	if runtime.GOOS != "linux" {
		t.Fatal("native goal isolation requires the fixed Linux runner")
	}
	version := os.Getenv("CODEX_CLI_VERSION")
	if version == "" {
		t.Fatal("CODEX_CLI_VERSION is required")
	}
	entry := os.Getenv("CODEX_BINARY")
	if entry == "" {
		entry = "codex"
	}
	binary, err := exec.LookPath(entry)
	if err != nil {
		t.Fatal("official Codex is required")
	}
	binary, err = filepath.EvalSymlinks(binary)
	if err != nil {
		t.Fatal("cannot resolve official Codex")
	}
	helper, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	helper, err = filepath.EvalSymlinks(helper)
	if err != nil {
		t.Fatal(err)
	}
	workspace := t.TempDir()
	if err = os.Mkdir(filepath.Join(workspace, ".git"), 0700); err != nil {
		t.Fatal(err)
	}
	feedback := t.TempDir()
	feedbackPath := filepath.Join(feedback, "feedback.jsonl")
	original := []byte("{\"fixture\":\"controller-feedback\"}\n")
	if err = os.WriteFile(feedbackPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Lstat(feedbackPath)
	if err != nil {
		t.Fatal(err)
	}
	hidden := t.TempDir()
	secret := filepath.Join(hidden, "parent-secret")
	if err = os.WriteFile(secret, []byte(goalParentCanary), 0600); err != nil {
		t.Fatal(err)
	}
	openSecret, err := os.Open(secret)
	if err != nil {
		t.Fatal(err)
	}
	defer openSecret.Close()
	socket := filepath.Join(hidden, "parent.sock")
	listener, err := net.Listen("unix", socket)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	t.Setenv("GH_TOKEN", goalParentCanary)
	t.Setenv("PROTOCOL_SYNC_APP_PRIVATE_KEY", goalParentCanary)
	t.Setenv("GH_CONFIG_DIR", hidden)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer native_fixture_only" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if err := os.WriteFile(filepath.Join(feedback, "later.jsonl"), []byte("later-feedback"), 0600); err != nil {
			w.WriteHeader(500)
			return
		}
		fmt.Fprint(w, "mock-provider-reachable")
	}))
	defer server.Close()
	runtimeDirs, err := NativeGoalRuntimePaths(binary)
	if err != nil {
		t.Fatal(err)
	}
	runtimeDirs = append(runtimeDirs, filepath.Dir(helper))
	moduleCache := os.Getenv("GOMODCACHE")
	if moduleCache == "" {
		cmd := exec.Command(filepath.Join(runtime.GOROOT(), "bin", "go"), "env", "GOMODCACHE")
		out, err := cmd.Output()
		if err != nil {
			t.Fatal("module cache resolution failed")
		}
		moduleCache = strings.TrimSpace(string(out))
	}
	for _, name := range []string{"go.mod", "go.sum"} {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", name))
		if err != nil {
			t.Fatal(err)
		}
		if os.WriteFile(filepath.Join(workspace, name), data, 0600) != nil {
			t.Fatal("cannot prepare module fixture")
		}
	}
	if err := os.WriteFile(filepath.Join(workspace, "main.go"), []byte(`package main
import ("fmt"; "github.com/pelletier/go-toml/v2")
func main(){ var value map[string]any; if toml.Unmarshal([]byte("probe=1"), &value)!=nil{panic("fixture")};fmt.Print("go-cache-ready") }
`), 0600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	e, err := New(ctx, Config{Workspace: workspace, StateDir: t.TempDir(), Binary: binary, Version: version, BaseURL: server.URL, APIKey: "native_fixture_only", Model: "fixture", Reasoning: "high", GoalSandbox: &GoalSandboxConfig{ModuleCache: moduleCache, RuntimeDirs: runtimeDirs, ReadOnlyDirs: []string{feedback}}})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()
	// Native helper receives only fixture paths and public runtime coordinates.
	// The parent canary itself is never supplied through argv or environment.
	cmd := e.command(ctx)
	cmd.Path = helper
	cmd.Args = []string{helper, "-test.run=^TestNativeGoalSandboxChild$", "--", "outer", workspace, feedback, secret, socket, strconv.Itoa(os.Getpid()), binary, server.URL}
	cmd, err = e.goalSandboxCommand(cmd)
	if err != nil {
		t.Fatal(err)
	}
	out, runErr := cmd.Output()
	after, statErr := os.Lstat(feedbackPath)
	data, readErr := os.ReadFile(feedbackPath)
	if statErr != nil || readErr != nil || !os.SameFile(before, after) || string(data) != string(original) {
		t.Fatal("native probe modified controller feedback")
	}
	if runErr != nil || string(out) != "native-goal-boundary-verified\n" {
		t.Fatalf("native goal boundary unverified: %v", runErr)
	}
	if b, err := os.ReadFile(filepath.Join(workspace, "inner-positive")); err != nil || string(b) != "inner-write" {
		t.Fatal("nested workspace positive control missing")
	}
	t.Run("cancellation", func(t *testing.T) {
		cancelCtx, stop := context.WithCancel(ctx)
		defer stop()
		blocked := e.command(cancelCtx)
		blocked.Path = helper
		blocked.Args = []string{helper, "-test.run=^TestNativeGoalSandboxChild$", "--", "block", workspace, feedback, secret, socket, strconv.Itoa(os.Getpid()), binary, server.URL}
		blocked, err = e.goalSandboxCommand(blocked)
		if err != nil {
			t.Fatal(err)
		}
		if err = blocked.Start(); err != nil {
			t.Fatal("native cancellation launch failed")
		}
		done := make(chan error, 1)
		go func() { done <- blocked.Wait() }()
		deadline := time.Now().Add(5 * time.Second)
		for {
			if _, err := os.Stat(filepath.Join(workspace, "native-heartbeat")); err == nil {
				break
			}
			if time.Now().After(deadline) {
				stop()
				<-done
				t.Fatal("native cancellation positive control missing")
			}
			time.Sleep(10 * time.Millisecond)
		}
		descendants := nativeDescendants(blocked.Process.Pid)
		if len(descendants) == 0 {
			stop()
			<-done
			t.Fatal("native child process observation missing")
		}
		stop()
		if err := <-done; err == nil {
			t.Fatal("cancelled native process reported success")
		}
		deadline = time.Now().Add(3 * time.Second)
		for _, pid := range descendants {
			for {
				if _, err := os.Stat(fmt.Sprintf("/proc/%d", pid)); os.IsNotExist(err) {
					break
				}
				if time.Now().After(deadline) {
					t.Fatal("native descendant survived cancellation")
				}
				time.Sleep(10 * time.Millisecond)
			}
		}
	})
}

const goalParentCanary = "goal-parent-credential-canary"

func TestNativeGoalSandboxChild(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--" {
			index = i
			break
		}
	}
	if index < 0 || len(os.Args) < index+9 {
		return
	}
	a := os.Args[index+1:]
	mode, workspace, feedback, secret, socket, parentPID, binary, url := a[0], a[1], a[2], a[3], a[4], a[5], a[6], a[7]
	fail := func(code int) { os.Exit(code) }
	if mode == "block" {
		descendant := exec.Command("/bin/sh", "-c", "while :; do printf alive > native-heartbeat; sleep 0.02; done")
		descendant.Dir = workspace
		if descendant.Start() != nil {
			fail(107)
		}
		for {
			time.Sleep(time.Hour)
		}
	}

	if os.Getenv("GH_TOKEN") != "" || os.Getenv("GH_CONFIG_DIR") != "" || os.Getenv("PROTOCOL_SYNC_APP_PRIVATE_KEY") != "" {
		fail(90)
	}
	if _, err := os.Stat(secret); !os.IsNotExist(err) {
		fail(91)
	}
	if data, err := os.ReadFile("/proc/" + parentPID + "/environ"); err == nil && strings.Contains(string(data), goalParentCanary) {
		fail(92)
	}
	processes, _ := filepath.Glob("/proc/[0-9]*/environ")
	for _, path := range processes {
		data, _ := os.ReadFile(path)
		if strings.Contains(string(data), goalParentCanary) {
			fail(93)
		}
	}
	descriptors, _ := filepath.Glob("/proc/self/fd/*")
	for _, path := range descriptors {
		target, _ := os.Readlink(path)
		if target == secret {
			fail(94)
		}
	}
	if connection, err := net.DialTimeout("unix", socket, 200*time.Millisecond); err == nil {
		connection.Close()
		fail(95)
	}
	path := filepath.Join(feedback, "feedback.jsonl")
	if data, err := os.ReadFile(path); err != nil || string(data) != "{\"fixture\":\"controller-feedback\"}\n" {
		fail(96)
	}
	if err := os.WriteFile(path, []byte("tamper"), 0600); err == nil {
		fail(97)
	}
	replacement := filepath.Join(workspace, "replacement.jsonl")
	if os.WriteFile(replacement, []byte("replacement"), 0600) != nil {
		fail(98)
	}
	if os.Rename(replacement, path) == nil {
		fail(99)
	}
	client := &http.Client{Timeout: 2 * time.Second}
	request, _ := http.NewRequest("GET", url, nil)
	request.Header.Set("Authorization", "Bearer "+os.Getenv("AZURE_OPENAI_API_KEY"))
	response, networkErr := client.Do(request)
	if mode == "inner" {
		if networkErr == nil {
			response.Body.Close()
			fail(100)
		}
		if os.Getenv("AZURE_OPENAI_API_KEY") != "" {
			fail(101)
		}
		if os.WriteFile(filepath.Join(workspace, "inner-positive"), []byte("inner-write"), 0600) != nil {
			fail(102)
		}
		if os.Getenv("GOPROXY") != "off" || os.Getenv("GOSUMDB") != "off" || os.Getenv("GOTOOLCHAIN") != "local" || !insideGoalPath(os.Getenv("GOCACHE"), workspace) || !insideGoalPath(os.Getenv("TMPDIR"), workspace) {
			fail(108)
		}
		build := exec.Command("go", "run", ".")
		build.Dir = workspace
		if output, err := build.Output(); err != nil || string(output) != "go-cache-ready" {
			fail(109)
		}
		fmt.Println("nested-native-verified")
		os.Exit(0)
	}
	if networkErr != nil || response.StatusCode != http.StatusOK {
		fail(103)
	}
	response.Body.Close()
	if data, err := os.ReadFile(filepath.Join(feedback, "later.jsonl")); err != nil || string(data) != "later-feedback" {
		fail(104)
	}
	if os.Getenv("HOME") != os.Getenv("CODEX_HOME") {
		fail(105)
	}
	helper, _ := os.Executable()
	args := []string{"sandbox", "-c", `sandbox_mode="workspace-write"`}
	args = append(args, (&Executor{}).sandboxOverrides()...)
	shellOverrides, err := (&Executor{config: Config{Workspace: workspace, GoalSandbox: &GoalSandboxConfig{ModuleCache: os.Getenv("GOMODCACHE")}}}).goalShellOverrides()
	if err != nil {
		fail(110)
	}
	args = append(args, shellOverrides...)
	args = append(args, "--", helper, "-test.run=^TestNativeGoalSandboxChild$", "--", "inner", workspace, feedback, secret, socket, parentPID, binary, url)
	native := exec.Command(binary, args...)
	native.Dir = workspace
	native.Env = os.Environ()
	out, err := native.Output()
	if err != nil || string(out) != "nested-native-verified\n" {
		fail(106)
	}
	fmt.Println("native-goal-boundary-verified")
	os.Exit(0)
}

// nativeDescendants observes only fixture-owned process IDs from the trusted
// parent namespace, so cancellation checks cover the namespace's grandchildren.
func nativeDescendants(pid int) []int {
	result := []int{}
	queue := []int{pid}
	for len(queue) > 0 && len(result) < 32 {
		current := queue[0]
		queue = queue[1:]
		data, err := os.ReadFile(fmt.Sprintf("/proc/%d/task/%d/children", current, current))
		if err != nil {
			continue
		}
		for _, field := range strings.Fields(string(data)) {
			child, err := strconv.Atoi(field)
			if err == nil {
				result = append(result, child)
				queue = append(queue, child)
			}
		}
	}
	return result
}

func TestGoalGoEnvironmentIsExplicitAndOffline(t *testing.T) {
	t.Setenv("GOMODCACHE", "/parent/SECRET-modules")
	t.Setenv("GOPROXY", "https://SECRET.invalid")
	e := goalFixture(t, "isolation")
	command, err := e.goalSandboxCommand(e.command(context.Background(), "app-server"))
	if err != nil {
		t.Fatal(err)
	}
	env := strings.Join(command.Env, "\n")
	for _, want := range []string{"GOMODCACHE=" + e.config.GoalSandbox.ModuleCache, "GOPROXY=off", "GOSUMDB=off", "GOTOOLCHAIN=local", "TMPDIR=/tmp"} {
		if !strings.Contains(env, want) {
			t.Fatalf("missing fixed environment: %s", want)
		}
	}
	if strings.Contains(env, "/parent/SECRET") || strings.Contains(env, "https://SECRET") {
		t.Fatal("ambient Go configuration inherited")
	}
	overrides, err := e.goalShellOverrides()
	if err != nil {
		t.Fatal(err)
	}
	var settings struct{ Set map[string]string }
	if err := toml.Unmarshal([]byte("set="+strings.TrimPrefix(overrides[1], "shell_environment_policy.set=")), &settings); err != nil {
		t.Fatal(err)
	}
	if len(settings.Set) != 6 || settings.Set["GOMODCACHE"] != e.config.GoalSandbox.ModuleCache || settings.Set["GOPROXY"] != "off" || settings.Set["GOSUMDB"] != "off" || settings.Set["GOTOOLCHAIN"] != "local" {
		t.Fatal("incorrect model shell environment")
	}
	for _, key := range []string{"GOCACHE", "TMPDIR"} {
		if !insideGoalPath(settings.Set[key], filepath.Join(e.config.Workspace, "codexsdk/.cache/codex-goal")) {
			t.Fatal("model cache escaped existing workspace grant")
		}
		if info, err := os.Stat(settings.Set[key]); err != nil || !info.IsDir() {
			t.Fatal("model cache directory missing")
		}
	}
	e.config.GoalSandbox.ModuleCache = ""
	if _, err := e.goalShellOverrides(); err == nil {
		t.Fatal("missing cache silently accepted")
	}
}

func TestGoalScratchRejectsSymlink(t *testing.T) {
	e := goalFixture(t, "isolation")
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(e.config.Workspace, "codexsdk"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(e.config.Workspace, "codexsdk/.cache")); err != nil {
		t.Fatal(err)
	}
	if _, err := e.goalSandboxCommand(e.command(context.Background(), "app-server")); err == nil {
		t.Fatal("candidate cache symlink accepted")
	}
	if _, err := os.Stat(filepath.Join(outside, "codex-goal")); !os.IsNotExist(err) {
		t.Fatal("wrote through candidate-controlled symlink")
	}
}
