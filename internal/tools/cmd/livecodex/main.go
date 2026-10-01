// livecodex is the shared local/CI entry point for the real Codex fixture.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"

	"github.com/ronhuafeng/llm-go/internal/tools/internal/livecodex"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: livecodex version | classify -base SHA -head SHA | run")
	}
	switch args[0] {
	case "version":
		version, err := livecodex.Version(".")
		if err != nil {
			return err
		}
		fmt.Println(version)
		return nil
	case "classify":
		flags := flag.NewFlagSet("classify", flag.ContinueOnError)
		base := flags.String("base", "", "comparison base")
		head := flags.String("head", "", "exact candidate")
		if err := flags.Parse(args[1:]); err != nil {
			return err
		}
		sha := regexp.MustCompile(`^[0-9a-f]{40}$`)
		if !sha.MatchString(*base) || !sha.MatchString(*head) {
			return errors.New("classify requires exact base and head commit SHAs")
		}
		data, err := exec.Command("git", "diff", "--name-only", "--no-renames", "-z", *base, *head).Output()
		if err != nil {
			return errors.New("cannot classify candidate diff")
		}
		paths := strings.Split(strings.TrimRight(string(data), "\x00"), "\x00")
		if len(data) == 0 {
			paths = nil
		}
		fmt.Println(livecodex.Required(paths))
		return nil
	case "run":
		key, err := livecodex.NormalizeKey(os.Getenv("AZURE_OPENAI_API_KEY"))
		if err != nil {
			fmt.Println("live_failure.stage=fixture")
			return err
		}
		version, err := livecodex.Version(".")
		if err != nil {
			return err
		}
		head, err := exec.Command("git", "rev-parse", "HEAD").Output()
		if err != nil {
			return errors.New("cannot identify tested repository revision")
		}
		fmt.Printf("live_revision=%s live_codex_release=%s live_configured_model=%s live_configured_reasoning=%s\n", strings.TrimSpace(string(head)), version, livecodex.Model, livecodex.Reasoning)
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Minute)
		defer cancel()
		command := exec.CommandContext(ctx, "go", "test", "./internal/tools/integration", "-run", "^TestLiveCodex", "-count=1", "-timeout=7m", "-json")
		for _, entry := range os.Environ() {
			name, _, _ := strings.Cut(entry, "=")
			switch name {
			case "GH_TOKEN", "GITHUB_TOKEN", "AUTO_FORWARD_APP_PRIVATE_KEY", "PROTOCOL_SYNC_APP_PRIVATE_KEY", "LLMGO_LIVE_CODEX", "AZURE_OPENAI_API_KEY":
				continue
			}
			command.Env = append(command.Env, entry)
		}
		command.Env = append(command.Env, "LLMGO_LIVE_CODEX=1", "AZURE_OPENAI_API_KEY="+key)
		command.Stderr = io.Discard
		pipe, err := command.StdoutPipe()
		if err != nil {
			return errors.New("cannot read live test events")
		}
		if err := command.Start(); err != nil {
			return errors.New("cannot start live test suite")
		}
		result := livecodex.CheckResults(pipe, os.Stdout)
		if result != nil {
			cancel()
		}
		if err := command.Wait(); err != nil {
			return errors.New("live_result=NOT_PROVEN: test process did not succeed; use emitted stage facts for diagnosis")
		}
		if result == nil {
			fmt.Println("live_result=PROVEN")
		}
		return result
	default:
		return errors.New("unknown livecodex command")
	}
}
