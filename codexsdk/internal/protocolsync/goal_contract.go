package protocolsync

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

// GoalContractRequest binds an original accepted baseline and freshly rebuilt
// upstream inputs to a frozen candidate. Only the caller chooses these paths.
type GoalContractRequest struct {
	RepoRoot, BaseRoot, CandidateDir string
	Output                           io.Writer
}

// GenerateGoalContract interprets candidate schemas only inside the existing
// no-network sandbox. The host consumes data, never imports candidate packages.
func GenerateGoalContract(ctx context.Context, req GoalContractRequest, out io.Writer) error {
	spec, err := schemaOraclePublicRequest(filepath.Join(req.BaseRoot, mechanicalPrefix), filepath.Join(req.CandidateDir, "schema"), filepath.Join(req.RepoRoot, "codexsdk/protocolv2"), filepath.Join(req.CandidateDir, "common.rs"))
	if err != nil {
		return err
	}
	spec.Context = ctx
	return schemaoracle.Write(spec, out)
}

func VerifyGoalContracts(ctx context.Context, req GoalContractRequest) error {
	for _, p := range []string{req.RepoRoot, req.BaseRoot, req.CandidateDir} {
		if !filepath.IsAbs(p) {
			return fmt.Errorf("contract inputs must be absolute")
		}
		resolved, err := filepath.EvalSymlinks(p)
		if err != nil || resolved != filepath.Clean(p) {
			return fmt.Errorf("contract input is missing or traverses a symlink")
		}
	}
	if req.RepoRoot == req.BaseRoot {
		return fmt.Errorf("contract baseline must be a separate accepted checkout")
	}
	temp, err := os.MkdirTemp("", "goal-contract-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(temp)
	root := filepath.Join(temp, "repo")
	if _, err = goalSnapshot(ctx, req.RepoRoot, root); err != nil {
		return err
	}
	if err = os.MkdirAll(filepath.Join(root, "codexsdk/.cache/openai-codex"), 0700); err != nil {
		return err
	}
	sandbox, err := newOracleSandbox(ctx, root, req.CandidateDir)
	if err != nil {
		return err
	}
	sandbox.readonly = append(sandbox.readonly, req.BaseRoot, root)
	driver := filepath.Join(root, "codexsdk/internal/goalcontractdriver")
	if err = os.Mkdir(driver, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(driver)
	if err = os.WriteFile(filepath.Join(driver, "main.go"), []byte(goalContractDriverSource), 0600); err != nil {
		return err
	}
	local, cancel := context.WithCancel(ctx)
	defer cancel()
	// Constructor and decoder have separate writable homes/caches. Candidate
	// selector code cannot replace the independently compiled decoder binary.
	constructor := sandbox
	constructor.work = filepath.Join(temp, "constructor")
	for _, p := range []string{constructor.work, filepath.Join(constructor.work, "cache"), filepath.Join(constructor.work, "home")} {
		if err = os.MkdirAll(p, 0700); err != nil {
			return err
		}
	}
	cmd := constructor.command(local, filepath.Join(sandbox.goroot, "bin/go"), "run", "./internal/goalcontractdriver", root, req.BaseRoot, req.CandidateDir)
	stream, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	defer stream.Close()
	output := &serializedOracleOutput{writer: io.Discard}
	if req.Output != nil {
		output.writer = req.Output
	}
	var stderr repairOutput
	cmd.Stderr = io.MultiWriter(&stderr, output)
	if err = cmd.Start(); err != nil {
		return err
	}
	reader := bufio.NewReader(stream)
	var header schemaoracle.Header
	contractErr := schemaoracle.ReadFrame(reader, &header)
	if contractErr == nil {
		contractErr = checkGoalContractCoverage(header, req)
	}
	request := oracleRequest{RepoRoot: root, BaseRoot: req.BaseRoot, ModuleRoot: filepath.Join(req.BaseRoot, "codexsdk"), CandidateDir: req.CandidateDir, Output: output}
	if contractErr == nil {
		contractErr = executeTargetOracle(local, request, root, sandbox, header, reader)
	}
	if contractErr != nil {
		cancel()
		stream.Close()
	}
	runErr := cmd.Wait()
	if contractErr != nil || runErr != nil {
		return fmt.Errorf("candidate contract construction/checking: %v process=%v: %s", contractErr, runErr, boundedRepairDiagnostic(stderr.String()))
	}
	if err = os.RemoveAll(driver); err != nil {
		return err
	}
	return runBaselineOracle(ctx, request, sandbox)
}

func baselineRoot(req oracleRequest) string {
	if req.BaseRoot != "" {
		return req.BaseRoot
	}
	return req.RepoRoot
}

// Copy only handwritten implementation, retaining the original baseline schema
// and test files. Enumerating both trees handles deletions without Git ancestry.
func overlayGoalImplementation(base, candidate string) error {
	names := map[string]bool{}
	for _, root := range []string{base, candidate} {
		paths, err := gitNUL(root, "ls-files", "-z")
		if err != nil {
			return err
		}
		for _, p := range paths {
			if isAgentProposalPath(p) && !strings.HasSuffix(p, "_test.go") {
				names[p] = true
			}
		}
	}
	for p := range names {
		from, to := filepath.Join(candidate, p), filepath.Join(base, p)
		info, err := os.Lstat(from)
		if os.IsNotExist(err) {
			if err = os.Remove(to); err != nil && !os.IsNotExist(err) {
				return err
			}
			continue
		}
		if err != nil {
			return err
		}
		real, err := filepath.EvalSymlinks(from)
		if err != nil || real != from || !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe implementation input %s", p)
		}
		data, err := os.ReadFile(from)
		if err != nil {
			return err
		}
		if err = os.MkdirAll(filepath.Dir(to), 0700); err != nil {
			return err
		}
		if err = os.WriteFile(to, data, 0600); err != nil {
			return err
		}
	}
	return nil
}

// Empty coverage never certifies an implementation change, even with identical
// schemas. Source byte digests bind every streamed expectation to the fixed U.
func checkGoalContractCoverage(header schemaoracle.Header, req GoalContractRequest) error {
	if len(header.Bindings) == 0 {
		return fmt.Errorf("contract coverage empty")
	}
	sources, err := goalContractTree(filepath.Join(req.CandidateDir, "schema"))
	if err != nil {
		return err
	}
	if len(sources) == 0 || !reflect.DeepEqual(sources, header.Sources) {
		return fmt.Errorf("contract source digest binding changed")
	}
	return nil
}
func goalContractTree(root string) (map[string]string, error) {
	files := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, e error) error {
		if e != nil {
			return e
		}
		if entry.IsDir() {
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("nonregular contract input")
		}
		rel, e := filepath.Rel(root, path)
		if e != nil {
			return e
		}
		rel = filepath.ToSlash(rel)
		if !strings.HasSuffix(rel, ".json") {
			return nil
		}
		switch rel {
		case "baseline_metadata.json", "coverage_matrix.json", "drift_report.json", "manifest.json", "manifest_generation.json", "matrix_update_skeleton.json", "codex_app_server_protocol.schemas.json", "codex_app_server_protocol.v2.schemas.json":
			return nil
		}
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		files[rel] = repairDigest(b)
		return nil
	})
	return files, err
}

const goalContractDriverSource = `package main
import("context";"os";"fmt"; p "github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync")
func main(){if err:=p.GenerateGoalContract(context.Background(),p.GoalContractRequest{RepoRoot:os.Args[1],BaseRoot:os.Args[2],CandidateDir:os.Args[3]},os.Stdout);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}}`
