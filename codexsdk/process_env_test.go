package codexsdk

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"os/signal"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/ronhuafeng/llm-go/codexsdk/protocolv2"
)

func TestChildEnvironmentNormalization(t *testing.T) {
	for _, windows := range []bool{false, true} {
		env, err := normalizeChildEnvironment(nil, windows)
		if err != nil || env != nil {
			t.Fatal("nil inheritance changed")
		}
		env, err = normalizeChildEnvironment([]string{}, windows)
		if err != nil || env == nil || len(env) != 0 {
			t.Fatal("empty replacement lost")
		}
		input := []string{"FIRST=literal=with=equals", "EMPTY=", "DUP=old", "DUP=new"}
		got, err := normalizeChildEnvironment(input, windows)
		if err != nil {
			t.Fatal(err)
		}
		if !reflect.DeepEqual(got, []string{"FIRST=literal=with=equals", "EMPTY=", "DUP=new"}) {
			t.Fatal("last-wins normalization changed")
		}
		input[0] = "mutated=value"
		if got[0] != "FIRST=literal=with=equals" {
			t.Fatal("input alias retained")
		}
		got, err = normalizeChildEnvironment([]string{"Key=first", "KEY=last"}, windows)
		if err != nil {
			t.Fatal(err)
		}
		expected := 2
		if windows {
			expected = 1
		}
		if len(got) != expected {
			t.Fatal("platform key matching changed")
		}
		got, err = normalizeChildEnvironment([]string{"KEY=first", "key=last"}, windows)
		if err != nil || len(got) != expected {
			t.Fatal("native Go Unicode key comparison changed")
		}
	}
	env, err := normalizeChildEnvironment([]string{"=C:=C:\\work", "=c:=C:\\other"}, true)
	if err != nil || len(env) != 1 || env[0] != "=c:=C:\\other" {
		t.Fatal("Windows hidden drive environment rejected")
	}
	if _, err = normalizeChildEnvironment([]string{"=C:=C:\\work"}, false); !errors.Is(err, ErrInvalidEnvironment) {
		t.Fatal("Unix accepted an empty environment name")
	}
	for _, entry := range []string{"no-separator-secret", "=secret", "NAME=private\x00value", "NAME\x00=secret", ""} {
		for _, windows := range []bool{false, true} {
			if _, err := normalizeChildEnvironment([]string{entry}, windows); !errors.Is(err, ErrInvalidEnvironment) || strings.Contains(err.Error(), "secret") || strings.Contains(err.Error(), "private") {
				t.Fatal("invalid entry not safely rejected")
			}
		}
	}
}

// This process fixture sends only hashes and presence bits for credentials.
// It needs no global environment opt-in, allowing genuinely concurrent clients.
func TestEnvironmentHelperProcess(t *testing.T) {
	index := -1
	for i, arg := range os.Args {
		if arg == "--env-fixture" {
			index = i
			break
		}
	}
	if index < 0 {
		return
	}
	mode := os.Getenv("CODEXSDK_FIXTURE_LIFECYCLE")
	if pidPath := os.Getenv("CODEXSDK_FIXTURE_PID"); pidPath != "" {
		if os.WriteFile(pidPath, []byte(strconv.Itoa(os.Getpid())), 0600) != nil {
			os.Exit(2)
		}
	}
	if mode == "unresponsive" {
		signal.Ignore(os.Interrupt)
	}
	scanner := bufio.NewScanner(os.Stdin)
	for scanner.Scan() {
		var message map[string]any
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			os.Exit(2)
		}
		method, _ := message["method"].(string)
		switch method {
		case "initialize":
			if mode == "stall-initialize" {
				for {
					time.Sleep(time.Hour)
				}
			}
			if mode == "eof-initialize" {
				os.Exit(0)
			}
			if mode == "invalid-initialize" {
				send(map[string]any{"id": message["id"], "result": map[string]any{"userAgent": 123}})
				continue
			}
			send(map[string]any{"id": message["id"], "result": map[string]any{"codexHome": os.Getenv("CODEX_HOME"), "platformFamily": "fixture", "platformOs": runtime.GOOS, "userAgent": "environment-fixture"}})
		case "thread/start":
			identity := os.Getenv("CODEXSDK_ENV_CORRELATION")
			sendProtocolResult(message["id"], facadeThreadStartResponse(identity, "fixture-model"))
		case "turn/start":
			identity := os.Getenv("CODEXSDK_ENV_CORRELATION")
			turnID := "turn-" + identity
			sendRawItem(identity, turnID, "before-response", false)
			sendEnvironmentNotification(protocolv2.NewServerNotificationThreadNameUpdated(protocolv2.ServerNotificationThreadNameUpdated{Params: protocolv2.ThreadNameUpdatedNotification{ThreadID: identity, ThreadName: protocolv2.Value("observed")}}))
			sendProtocolResult(message["id"], protocolv2.TurnStartResponse{Turn: protocolv2.Turn{ID: turnID, Status: protocolv2.TurnStatusInProgress, Items: []protocolv2.ThreadItem{}}})
			count, _ := strconv.Atoi(os.Getenv("CODEXSDK_FIXTURE_EVENTS"))
			if mode == "blocked-work" {
				continue
			}
			for i := 0; i < count; i++ {
				sendRawItem(identity, turnID, strconv.Itoa(i/2), false)
				n := tokenUsageNotification(identity, turnID)
				send(map[string]any{"method": n.method, "params": n.params})
			}
			sendEnvironmentNotification(protocolv2.NewServerNotificationTurnCompleted(protocolv2.ServerNotificationTurnCompleted{Params: protocolv2.TurnCompletedNotification{ThreadID: identity, Turn: protocolv2.Turn{ID: turnID, Status: protocolv2.TurnStatusCompleted, Items: []protocolv2.ThreadItem{}}}}))
		case "fixture/finish":
			identity := os.Getenv("CODEXSDK_ENV_CORRELATION")
			sendEnvironmentNotification(protocolv2.NewServerNotificationTurnCompleted(protocolv2.ServerNotificationTurnCompleted{Params: protocolv2.TurnCompletedNotification{ThreadID: identity, Turn: protocolv2.Turn{ID: "turn-" + identity, Status: protocolv2.TurnStatusCompleted, Items: []protocolv2.ThreadItem{}}}}))
			send(map[string]any{"id": message["id"], "result": map[string]any{}})
		case "fixture/environment":
			observations := map[string]any{}
			for _, key := range []string{"CODEX_HOME", "CODEXSDK_ENV_SECRET", "CODEXSDK_ENV_EMPTY", "CODEXSDK_ENV_REMOVE", "CODEXSDK_ENV_PARENT", "CODEXSDK_ENV_CASE"} {
				value, present := os.LookupEnv(key)
				observations[key] = map[string]any{"present": present, "digest": environmentDigest(value)}
			}
			secret := os.Getenv("CODEXSDK_ENV_SECRET")
			safeArgs := secret == "" || !strings.Contains(strings.Join(os.Args, "\n"), secret)
			observations["safeArgs"] = safeArgs
			send(map[string]any{"id": message["id"], "result": observations})
		case "fixture/budget-notifications":
			identity := os.Getenv("CODEXSDK_ENV_CORRELATION")
			params, _ := message["params"].(map[string]any)
			marker := "admitted"
			if params["oversize"] == true {
				marker = strings.Repeat("payload-sentinel", 4096)
			}
			sendRawItem(identity, "turn-"+identity, marker, false)
			n := tokenUsageNotification(identity, "turn-"+identity)
			send(map[string]any{"method": n.method, "params": n.params})
			send(map[string]any{"id": message["id"], "result": map[string]any{}})
		case "model/list":
			if mode == "unresponsive" || mode == "blocked-work" {
				for {
					time.Sleep(time.Hour)
				}
			}
			if mode == "combined" {
				sendProtocolResult(message["id"], protocolv2.ModelListResponse{Data: []protocolv2.Model{{ID: "fixture-model", Model: "fixture-model", DisplayName: "Fixture", Description: "typed observation", DefaultReasoningEffort: protocolv2.ReasoningEffort("high"), IsDefault: true, SupportedReasoningEfforts: []protocolv2.ReasoningEffortOption{}}}})
			} else {
				sendProtocolResult(message["id"], protocolv2.ModelListResponse{Data: []protocolv2.Model{}})
			}
		}
	}
	os.Exit(0)
}

func environmentDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
func environmentCommand() []string {
	return []string{os.Args[0], "-test.run=^TestEnvironmentHelperProcess$", "--", "--env-fixture"}
}
func environmentFacts(t *testing.T, c *Client) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	facts, err := c.call(ctx, "fixture/environment", nil)
	if err != nil {
		t.Fatal("environment fixture query failed")
	}
	return facts
}
func requireEnvironmentFact(t *testing.T, facts map[string]any, key, value string, present bool) {
	t.Helper()
	fact, ok := facts[key].(map[string]any)
	if !ok || fact["present"] != present || fact["digest"] != environmentDigest(value) {
		t.Fatalf("child environment mismatch: key=%s", key)
	}
}

func TestChildEnvironmentInheritanceReplacementRemovalAndEmpty(t *testing.T) {
	t.Setenv("CODEXSDK_ENV_PARENT", "parent-value")
	t.Setenv("CODEXSDK_ENV_REMOVE", "remove-value")
	t.Setenv("CODEXSDK_ENV_SECRET", "parent-private-sentinel")
	t.Setenv("CODEX_HOME", t.TempDir())
	c, err := New(ClientOptions{CWD: t.TempDir(), Command: environmentCommand()})
	if err != nil {
		t.Fatal("inherited client construction failed")
	}
	facts := environmentFacts(t, c)
	requireEnvironmentFact(t, facts, "CODEXSDK_ENV_PARENT", "parent-value", true)
	requireEnvironmentFact(t, facts, "CODEXSDK_ENV_REMOVE", "remove-value", true)
	requireEnvironmentFact(t, facts, "CODEX_HOME", os.Getenv("CODEX_HOME"), true)
	if err = c.Close(); err != nil {
		t.Fatal("inherited child cleanup failed")
	}
	home := t.TempDir()
	env := []string{"CODEX_HOME=" + home, "CODEXSDK_ENV_SECRET=child-private-sentinel", "CODEXSDK_ENV_EMPTY=", "CODEXSDK_ENV_CASE=first", "CODEXSDK_ENV_CASE=last"}
	c, err = New(ClientOptions{CWD: t.TempDir(), Command: environmentCommand(), Env: env})
	if err != nil {
		t.Fatal("replacement client construction failed")
	}
	facts = environmentFacts(t, c)
	requireEnvironmentFact(t, facts, "CODEX_HOME", home, true)
	requireEnvironmentFact(t, facts, "CODEXSDK_ENV_SECRET", "child-private-sentinel", true)
	requireEnvironmentFact(t, facts, "CODEXSDK_ENV_EMPTY", "", true)
	requireEnvironmentFact(t, facts, "CODEXSDK_ENV_REMOVE", "", false)
	requireEnvironmentFact(t, facts, "CODEXSDK_ENV_PARENT", "", false)
	requireEnvironmentFact(t, facts, "CODEXSDK_ENV_CASE", "last", true)
	if facts["safeArgs"] != true {
		t.Fatal("credential entered child argv")
	}
	env[0] = "CODEX_HOME=mutated"
	env[1] = "CODEXSDK_ENV_SECRET=mutated"
	facts = environmentFacts(t, c)
	requireEnvironmentFact(t, facts, "CODEX_HOME", home, true)
	requireEnvironmentFact(t, facts, "CODEXSDK_ENV_SECRET", "child-private-sentinel", true)
	if err = c.Close(); err != nil {
		t.Fatal("replacement child cleanup failed")
	}
	c, err = New(ClientOptions{CWD: t.TempDir(), Command: environmentCommand(), Env: []string{}})
	if err != nil {
		t.Fatal("empty environment construction failed")
	}
	facts = environmentFacts(t, c)
	for _, key := range []string{"CODEX_HOME", "CODEXSDK_ENV_SECRET", "CODEXSDK_ENV_PARENT", "CODEXSDK_ENV_EMPTY"} {
		requireEnvironmentFact(t, facts, key, "", false)
	}
	if err = c.Close(); err != nil {
		t.Fatal("empty environment cleanup failed")
	}
	if os.Getenv("CODEXSDK_ENV_SECRET") != "parent-private-sentinel" {
		t.Fatal("parent environment changed")
	}
}

func TestConcurrentClientsIsolateChildEnvironments(t *testing.T) {
	t.Setenv("CODEXSDK_ENV_PARENT", "unchanged-parent")
	t.Setenv("CODEXSDK_ENV_SECRET", "unchanged-private-parent")
	before := os.Environ()
	type launch struct{ cwd, home, secret string }
	launches := []launch{{t.TempDir(), t.TempDir(), "private-child-one"}, {t.TempDir(), t.TempDir(), "private-child-two"}}
	start := make(chan struct{})
	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	var wg sync.WaitGroup
	failures := make(chan string, 8)
	for _, spec := range launches {
		wg.Add(1)
		go func(spec launch) {
			defer wg.Done()
			<-start
			env := []string{"CODEX_HOME=" + spec.home, "CODEXSDK_ENV_SECRET=" + spec.secret}
			c, err := New(ClientOptions{CWD: spec.cwd, Command: environmentCommand(), Env: env})
			if err != nil {
				failures <- "construction failed"
				ready <- struct{}{}
				return
			}
			defer c.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			facts, err := c.call(ctx, "fixture/environment", nil)
			if err != nil {
				failures <- "query failed"
			} else {
				for key, want := range map[string]string{"CODEX_HOME": spec.home, "CODEXSDK_ENV_SECRET": spec.secret} {
					fact, ok := facts[key].(map[string]any)
					if !ok || fact["present"] != true || fact["digest"] != environmentDigest(want) {
						failures <- "isolation failed"
						break
					}
				}
				if facts["safeArgs"] != true {
					failures <- "unsafe arguments"
				}
			}
			ready <- struct{}{}
			<-release
			if _, err = c.Models().List(ctx, protocolv2.ModelListParams{}); err != nil {
				failures <- "typed facade failed"
			}
		}(spec)
	}
	close(start)
	for range 2 {
		<-ready
	}
	if !reflect.DeepEqual(before, os.Environ()) {
		close(release)
		wg.Wait()
		t.Fatal("parent changed during simultaneous clients")
	}
	close(release)
	wg.Wait()
	close(failures)
	for failure := range failures {
		t.Error(failure)
	}
	if !reflect.DeepEqual(before, os.Environ()) {
		t.Fatal("parent changed after child cleanup")
	}
}

func TestChildEnvironmentInvalidInputPreventsLaunch(t *testing.T) {
	_, err := New(ClientOptions{CWD: t.TempDir(), Command: []string{"must-not-launch"}, Env: []string{"PRIVATE_SECRET=unsafe\x00value"}})
	if !errors.Is(err, ErrInvalidEnvironment) || strings.Contains(err.Error(), "PRIVATE_SECRET") || strings.Contains(err.Error(), "unsafe") {
		t.Fatal("invalid environment was not rejected safely before launch")
	}
}

func TestChildEnvironmentStartupFailureDoesNotDiscloseValues(t *testing.T) {
	sentinel := "private-child-startup-sentinel"
	_, err := New(ClientOptions{CWD: t.TempDir(), Command: []string{t.TempDir() + "/missing"}, Env: []string{"CODEXSDK_ENV_SECRET=" + sentinel}})
	if err == nil || strings.Contains(err.Error(), sentinel) {
		t.Fatal("startup failure disclosed environment or incorrectly succeeded")
	}
}

func TestChildEnvironmentOptionsCopyBeforeLaunch(t *testing.T) {
	input := []string{"CODEX_HOME=private-home", "TOKEN=private-token"}
	options, err := validateOptions(ClientOptions{CWD: t.TempDir(), Command: environmentCommand(), Env: input})
	if err != nil {
		t.Fatal("options validation failed")
	}
	input[0] = "CODEX_HOME=changed"
	input[1] = "TOKEN=changed"
	if options.Env[0] != "CODEX_HOME=private-home" || options.Env[1] != "TOKEN=private-token" {
		t.Fatal("normalized options retain caller-owned environment")
	}
	options, err = validateOptions(ClientOptions{CWD: t.TempDir(), Command: environmentCommand(), Env: []string{}})
	if err != nil || options.Env == nil {
		t.Fatal("validation changed empty replacement into inheritance")
	}
}

func TestChildEnvironmentClientCloseReapsOwnedProcess(t *testing.T) {
	c, err := New(ClientOptions{CWD: t.TempDir(), Command: environmentCommand(), Env: []string{"CODEX_HOME=" + t.TempDir(), "CODEXSDK_ENV_SECRET=cleanup-private-sentinel"}})
	if err != nil {
		t.Fatal("child construction failed")
	}
	if err = c.Close(); err != nil {
		t.Fatal("child cleanup failed")
	}
	if c.cmd.ProcessState == nil {
		t.Fatal("owned child was not reaped")
	}
	if err = c.Close(); err != nil {
		t.Fatal("repeated child cleanup failed")
	}
}

func sendEnvironmentNotification(n protocolv2.ServerNotification) {
	raw, err := json.Marshal(n)
	if err != nil {
		os.Exit(2)
	}
	_, _ = os.Stdout.Write(append(raw, '\n'))
}
