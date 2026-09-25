package protocolsync

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolupgrade"
)

const defaultBaselineRel = "internal/protocolschema/appserver/v2"

// Candidate is a generated schema snapshot plus drift reports.
type Candidate struct {
	Dir               string
	SchemaDir         string
	StableSchemaDir   string
	ReportsDir        string
	CommonRS          string
	CommonRSSourceSHA string
	CodexRepo         string
	DriftStatus       string
	SourceCommit      string
}

// GenerateRequest acquires the selected Codex source and generates schemas.
type GenerateRequest struct {
	ModuleRoot   string
	UpstreamRepo string
	Target       Target
}

// GenerateCandidate fetches the selected Codex commit, generates schemas, and compares.
func GenerateCandidate(req GenerateRequest) (Candidate, error) {
	moduleRoot, err := filepath.Abs(req.ModuleRoot)
	if err != nil {
		return Candidate{}, err
	}
	targetSHA := req.Target.PeeledCommitSHA
	syncOut := filepath.Join(moduleRoot, ".cache", "codexsdk-upstream-"+targetSHA[:12])
	codexRepo := filepath.Join(moduleRoot, ".cache", "openai-codex")
	if err := prepareUpstreamRepo(codexRepo, req.UpstreamRepo); err != nil {
		return Candidate{}, err
	}
	if err := fetchCommit(codexRepo, targetSHA); err != nil {
		return Candidate{}, err
	}
	schemaDir := filepath.Join(syncOut, "schema")
	stableDir := filepath.Join(syncOut, "stable-schema")
	reportsDir := filepath.Join(syncOut, "reports")
	worktree := filepath.Join(syncOut, "codex-worktree")
	if err := os.RemoveAll(schemaDir); err != nil {
		return Candidate{}, err
	}
	if err := os.RemoveAll(stableDir); err != nil {
		return Candidate{}, err
	}
	if err := os.RemoveAll(reportsDir); err != nil {
		return Candidate{}, err
	}
	if err := os.RemoveAll(worktree); err != nil {
		return Candidate{}, err
	}
	for _, dir := range []string{schemaDir, stableDir, reportsDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Candidate{}, err
		}
	}
	if err := runGit(codexRepo, "worktree", "add", "--detach", worktree, targetSHA); err != nil {
		return Candidate{}, err
	}
	defer func() {
		_ = runGit(codexRepo, "worktree", "remove", "--force", worktree)
	}()

	codexRS := filepath.Join(worktree, "codex-rs")
	if _, err := os.Stat(codexRS); err != nil {
		return Candidate{}, fmt.Errorf("codex worktree missing codex-rs at %s: %w", codexRS, err)
	}
	rustupHome := filepath.Join(moduleRoot, ".cache", "rustup")
	cargoHome := filepath.Join(moduleRoot, ".cache", "cargo-home")
	cargoTarget := filepath.Join(moduleRoot, ".cache", "cargo-target", "codex")
	for _, dir := range []string{rustupHome, cargoHome, cargoTarget} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return Candidate{}, err
		}
	}
	env := append(os.Environ(),
		"RUSTUP_HOME="+rustupHome,
		"CARGO_HOME="+cargoHome,
		"CARGO_TARGET_DIR="+cargoTarget,
	)
	if err := runCargo(codexRS, env, "run", "-p", "codex-cli", "--", "app-server", "generate-json-schema", "--experimental", "--out", schemaDir); err != nil {
		return Candidate{}, err
	}
	if err := runCargo(codexRS, env, "run", "-p", "codex-cli", "--", "app-server", "generate-json-schema", "--out", stableDir); err != nil {
		return Candidate{}, err
	}
	versionCmd := exec.Command("cargo", "run", "-p", "codex-cli", "--", "--version")
	versionCmd.Dir = codexRS
	versionCmd.Env = env
	versionOut, err := versionCmd.Output()
	if err != nil {
		return Candidate{}, fmt.Errorf("read exact upstream codex-cli version: %w", err)
	}
	codexVersion := strings.TrimSpace(string(versionOut))
	if !strings.HasPrefix(codexVersion, "codex-cli ") {
		return Candidate{}, fmt.Errorf("unexpected exact upstream codex-cli version %q", codexVersion)
	}
	commonRS := filepath.Join(syncOut, "common.rs")
	if err := writeGitShow(codexRepo, targetSHA+":codex-rs/app-server-protocol/src/protocol/common.rs", commonRS); err != nil {
		return Candidate{}, err
	}
	if err := os.WriteFile(filepath.Join(syncOut, "common.rs.source_sha"), []byte(targetSHA+"\n"), 0o644); err != nil {
		return Candidate{}, err
	}

	baseline := filepath.Join(moduleRoot, filepath.FromSlash(defaultBaselineRel))
	report, err := protocolupgrade.Compare(protocolupgrade.CompareRequest{
		Baseline:        baseline,
		Candidate:       schemaDir,
		SourceCommit:    targetSHA,
		SourceRef:       req.Target.RefName,
		SourceRefKind:   req.Target.RefKind,
		CodexVersion:    codexVersion,
		Generator:       "cargo",
		GeneratorDetail: filepath.Join(worktree, "codex-rs") + " cargo run -p codex-cli",
	})
	if err != nil {
		return Candidate{}, err
	}
	if _, err := protocolupgrade.WriteReports(reportsDir, report, schemaDir); err != nil {
		return Candidate{}, err
	}
	return Candidate{
		Dir:               syncOut,
		SchemaDir:         schemaDir,
		StableSchemaDir:   stableDir,
		ReportsDir:        reportsDir,
		CommonRS:          commonRS,
		CommonRSSourceSHA: targetSHA,
		CodexRepo:         codexRepo,
		DriftStatus:       report.Status,
		SourceCommit:      targetSHA,
	}, nil
}

func prepareUpstreamRepo(codexRepo, upstreamRepo string) error {
	gitDir := filepath.Join(codexRepo, ".git")
	if _, err := os.Stat(gitDir); err == nil {
		origin, err := gitOutput(codexRepo, "remote", "get-url", "origin")
		if err != nil {
			return err
		}
		if strings.TrimSpace(origin) != upstreamRepo {
			return fmt.Errorf("cached Codex origin %s does not match %s", strings.TrimSpace(origin), upstreamRepo)
		}
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(codexRepo), 0o755); err != nil {
		return err
	}
	if err := runGit("", "init", "--quiet", codexRepo); err != nil {
		return err
	}
	return runGit(codexRepo, "remote", "add", "origin", upstreamRepo)
}

func fetchCommit(codexRepo, sha string) error {
	if err := runGit(codexRepo, "rev-parse", "--verify", "-q", sha+"^{commit}"); err == nil {
		return nil
	}
	return runGit(codexRepo, "fetch", "origin", "+"+sha+":refs/codexsdk/target")
}

func runCargo(dir string, env []string, args ...string) error {
	cmd := exec.Command("cargo", args...)
	cmd.Dir = dir
	cmd.Env = env
	cmd.Stdout = os.Stderr
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("cargo %s: %w", strings.Join(args, " "), err)
	}
	return nil
}

func runGit(dir string, args ...string) error {
	cmdArgs := args
	if dir != "" {
		cmdArgs = append([]string{"-C", dir}, args...)
	}
	cmd := exec.Command("git", cmdArgs...)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, bytesTrim(out))
	}
	return nil
}

func gitOutput(dir string, args ...string) (string, error) {
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func writeGitShow(repo, spec, dest string) error {
	cmd := exec.Command("git", "-C", repo, "show", spec)
	out, err := cmd.Output()
	if err != nil {
		return fmt.Errorf("git show %s: %w", spec, err)
	}
	return os.WriteFile(dest, out, 0o644)
}

func bytesTrim(out []byte) string {
	return strings.TrimSpace(string(out))
}
