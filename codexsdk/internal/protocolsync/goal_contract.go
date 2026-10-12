package protocolsync

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/schemaoracle"
)

// GoalContractRequest binds an original accepted baseline and freshly rebuilt
// upstream inputs to a frozen candidate. Only the caller chooses these paths.
type GoalContractRequest struct {
	RepoRoot, BaseRoot, CandidateDir string
	Output                           io.Writer
}

// GenerateGoalContract runs as candidate code inside the existing sandbox. Both
// source mapping and schema expectations therefore evolve in the same reviewed PR.
func GenerateGoalContract(ctx context.Context, req GoalContractRequest, destination string, failures ...string) error {
	spec, err := schemaOraclePublicRequest(filepath.Join(req.BaseRoot, mechanicalPrefix), filepath.Join(req.CandidateDir, "schema"), filepath.Join(req.RepoRoot, "codexsdk/protocolv2"), filepath.Join(req.CandidateDir, "common.rs"))
	if err != nil {
		return err
	}
	spec.Context = ctx
	return buildGoalContract(spec, destination, failures...)
}

func buildGoalContract(spec schemaoracle.Request, destination string, failures ...string) error {
	var targets []schemaoracle.Target
	var source []byte
	var err error
	if len(spec.Bindings) > 0 {
		targets, err = schemaoracle.Build(spec)
		if err != nil {
			return err
		}
		source, err = schemaoracle.Render(spec, targets)
		if err != nil {
			return err
		}
	}
	if len(failures) > 0 {
		if repairDigest(source) != failures[0] {
			return fmt.Errorf("contract reconstruction source digest changed")
		}
		f, err := os.Create(filepath.Join(destination, "failure-metadata.jsonl"))
		if err != nil {
			return err
		}
		defer f.Close()
		if err = writeGoalContractFailures(f, targets, failures[1:]); err != nil {
			return err
		}
	}
	return os.WriteFile(filepath.Join(destination, "contract_test.go"), source, 0600)
}

// VerifyGoalContracts is host orchestration; candidate Go code executes only in
// the existing no-network sandbox. It does not invoke a nested sandbox.
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
	sandbox.readonly = append(sandbox.readonly, req.BaseRoot)
	driver := filepath.Join(root, "codexsdk/internal/goalcontractdriver")
	if err = os.Mkdir(driver, 0700); err != nil {
		return err
	}
	defer os.RemoveAll(driver)
	// The fixed driver imports the candidate package, including its source selector.
	if err = os.WriteFile(filepath.Join(driver, "main.go"), []byte(goalContractDriverSource), 0600); err != nil {
		return err
	}
	sandbox.readonly = append(sandbox.readonly, root)
	cmd := sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin/go"), "run", "./internal/goalcontractdriver", root, req.BaseRoot, req.CandidateDir, sandbox.work)
	if _, err = oracleCommandOutput(req.Output, cmd); err != nil {
		return fmt.Errorf("candidate contract construction: %w", err)
	}
	source, err := readGoalContractArtifact(filepath.Join(sandbox.work, "contract_test.go"), 12<<20)
	if err != nil {
		return err
	}
	if err = checkGoalContractCoverage(source, req); err != nil {
		return err
	}
	var failures []string
	request := oracleRequest{RepoRoot: root, BaseRoot: req.BaseRoot, ModuleRoot: filepath.Join(req.BaseRoot, "codexsdk"), CandidateDir: req.CandidateDir, Output: req.Output}
	request.failure = func(test string) error {
		if test != "" {
			failures = append(failures, test)
		}
		return nil
	}
	if len(source) > 0 {
		if assertionErr := executeTargetOracle(ctx, request, root, sandbox, source, nil); assertionErr != nil {
			if len(failures) == 0 {
				return fmt.Errorf("contract failed without complete failure IDs: %w", assertionErr)
			}
			args := []string{"run", "./internal/goalcontractdriver", root, req.BaseRoot, req.CandidateDir, sandbox.work, repairDigest(source)}
			namesPath := filepath.Join(sandbox.work, "failed-tests.json")
			namesJSON, err := json.Marshal(failures)
			if err != nil {
				return err
			}
			if err = os.WriteFile(namesPath, namesJSON, 0600); err != nil {
				return err
			}
			sandbox.readonly = append(sandbox.readonly, namesPath)
			args = append(args, namesPath)
			cmd = sandbox.command(ctx, filepath.Join(sandbox.goroot, "bin/go"), args...)
			if _, err = oracleCommandOutput(req.Output, cmd); err != nil {
				return fmt.Errorf("contract metadata reconstruction incomplete: %w; assertion: %v", err, assertionErr)
			}
			rebuilt, err := readGoalContractArtifact(filepath.Join(sandbox.work, "contract_test.go"), 12<<20)
			if err != nil {
				return err
			}
			if repairDigest(rebuilt) != repairDigest(source) {
				return fmt.Errorf("contract reconstruction source digest changed")
			}
			data, err := readGoalContractArtifact(filepath.Join(sandbox.work, "failure-metadata.jsonl"), 128<<20)
			if err != nil {
				return err
			}
			if err = forwardGoalContractFailures(request.output(), data, failures); err != nil {
				return err
			}
			return assertionErr
		}
	}
	// Driver is not an implementation input to the accepted baseline lane.
	if err = os.RemoveAll(driver); err != nil {
		return err
	}
	return runBaselineOracle(ctx, request, sandbox)
}

func readGoalContractArtifact(path string, limit int64) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("invalid contract artifact")
	}
	return os.ReadFile(path)
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

// Only actual failed tests cross the process boundary with their full metadata.
// Parent/root failure events are retained too, so missing or unknown IDs block.
func writeGoalContractFailures(out io.Writer, targets []schemaoracle.Target, failures []string) error {
	enc := json.NewEncoder(out)
	for _, name := range failures {
		var body bytes.Buffer
		found := name == "TestTrustedSchemaOracle"
		for _, target := range targets {
			prefix := "TestTrustedSchemaOracle/" + target.Schema
			if name == prefix {
				found = true
				break
			}
			if name == prefix+"/fields" {
				found = true
				if err := json.NewEncoder(&body).Encode(map[string]any{"kind": "schema_field_expectation", "schema": target.Schema, "fields": target.Fields}); err != nil {
					return err
				}
				break
			}
			if strings.HasPrefix(name, prefix+"/probe_") {
				index, err := strconv.Atoi(strings.TrimPrefix(name, prefix+"/probe_"))
				if err != nil || index < 0 || index >= len(target.Probes) {
					return fmt.Errorf("unknown failed probe %q", name)
				}
				found = true
				if err = writeTargetOracleProbe(&body, targets, name); err != nil {
					return err
				}
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown failed contract test %q", name)
		}
		raw := json.RawMessage(bytes.TrimSpace(body.Bytes()))
		if len(raw) == 0 {
			raw = json.RawMessage("null")
		}
		if err := enc.Encode(goalContractFailure{Name: name, Expectation: raw}); err != nil {
			return err
		}
	}
	return nil
}

type goalContractFailure struct {
	Name        string          `json:"test"`
	Expectation json.RawMessage `json:"expectation"`
}

func forwardGoalContractFailures(out io.Writer, data []byte, names []string) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	var records []goalContractFailure
	for _, name := range names {
		var r goalContractFailure
		if err := decoder.Decode(&r); err != nil {
			return fmt.Errorf("incomplete reconstructed metadata: %w", err)
		}
		if r.Name != name || len(r.Expectation) == 0 || ((strings.Contains(name, "/probe_") || strings.HasSuffix(name, "/fields")) && string(r.Expectation) == "null") {
			return fmt.Errorf("mismatched reconstructed metadata")
		}
		records = append(records, r)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		return fmt.Errorf("unexpected reconstructed metadata")
	}
	for _, r := range records {
		if string(r.Expectation) != "null" {
			if _, err := out.Write(append(r.Expectation, '\n')); err != nil {
				return err
			}
		}
	}
	return nil
}

// Empty selection is accepted only for the same independently bound upstream
// source with byte-identical raw schemas and public protocol tree. No schema
// interpretation or candidate selector participates in this no-drift proof.
func checkGoalContractCoverage(source []byte, req GoalContractRequest) error {
	if len(bytes.TrimSpace(source)) > 0 {
		return nil
	}
	commit := func(root string) (string, error) {
		b, e := os.ReadFile(filepath.Join(root, mechanicalPrefix, "baseline_metadata.json"))
		if e != nil {
			return "", e
		}
		var m struct {
			Commit string `json:"source_commit"`
		}
		if e = json.Unmarshal(b, &m); e != nil {
			return "", e
		}
		raw, e := hex.DecodeString(m.Commit)
		if e != nil || len(raw) != 20 {
			return "", fmt.Errorf("invalid upstream source identity")
		}
		return m.Commit, nil
	}
	before, err := commit(req.BaseRoot)
	if err != nil {
		return fmt.Errorf("contract coverage empty: %w", err)
	}
	after, err := commit(req.RepoRoot)
	if err != nil {
		return fmt.Errorf("contract coverage empty: %w", err)
	}
	if before != after {
		return fmt.Errorf("contract coverage empty: upstream source changed")
	}
	for _, view := range []struct {
		old, next string
		schema    bool
	}{
		{filepath.Join(req.BaseRoot, mechanicalPrefix), filepath.Join(req.CandidateDir, "schema"), true},
		{filepath.Join(req.BaseRoot, "codexsdk/protocolv2"), filepath.Join(req.RepoRoot, "codexsdk/protocolv2"), false},
	} {
		a, e := goalContractTree(view.old, view.schema)
		if e != nil {
			return fmt.Errorf("contract coverage empty: %w", e)
		}
		b, e := goalContractTree(view.next, view.schema)
		if e != nil {
			return fmt.Errorf("contract coverage empty: %w", e)
		}
		if len(a) == 0 || !reflect.DeepEqual(a, b) {
			return fmt.Errorf("contract coverage empty: schema or public protocol bytes changed")
		}
	}
	return nil
}
func goalContractTree(root string, schema bool) (map[string]string, error) {
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
		if schema {
			if !strings.HasSuffix(rel, ".json") {
				return nil
			}
			switch rel {
			case "baseline_metadata.json", "coverage_matrix.json", "drift_report.json", "manifest.json", "manifest_generation.json", "matrix_update_skeleton.json":
				return nil
			}
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

// Shared with the real compile regression; these are the exact runtime bytes.
const goalContractDriverSource = `package main
import("context";"os";"fmt";"encoding/json"; p "github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync")
func main(){var failures []string;if len(os.Args)>5 {b,e:=os.ReadFile(os.Args[6]);if e==nil{e=json.Unmarshal(b,&failures)};if e!=nil{fmt.Fprintln(os.Stderr,e);os.Exit(1)};failures=append([]string{os.Args[5]},failures...)};if err:=p.GenerateGoalContract(context.Background(),p.GoalContractRequest{RepoRoot:os.Args[1],BaseRoot:os.Args[2],CandidateDir:os.Args[3]},os.Args[4],failures...);err!=nil{fmt.Fprintln(os.Stderr,err);os.Exit(1)}}`
