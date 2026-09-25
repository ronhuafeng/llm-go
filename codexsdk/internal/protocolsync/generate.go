package protocolsync

import (
	"bytes"
	"crypto/sha256"
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
	// Build-input evidence belongs to this attempt, including when acquisition
	// or preparation fails before producing a new prepared lockfile.
	for _, name := range []string{"upstream.Cargo.lock", "prepared.Cargo.lock"} {
		if err := os.Remove(filepath.Join(syncOut, name)); err != nil && !os.IsNotExist(err) {
			return Candidate{}, err
		}
	}
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
	// Cargo searches ancestor directories and CARGO_HOME for configuration.
	// Only configuration in the selected source is a build input; caches
	// must not silently supply additional compiler or dependency settings.
	configDirs := []string{cargoHome}
	for dir := filepath.Dir(worktree); ; dir = filepath.Dir(dir) {
		configDirs = append(configDirs, filepath.Join(dir, ".cargo"))
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for _, dir := range configDirs {
		for _, name := range []string{"config", "config.toml"} {
			path := filepath.Join(dir, name)
			if _, err := os.Stat(path); err == nil {
				return Candidate{}, &Failure{Category: FailureSource, Err: fmt.Errorf("external Cargo configuration is not a selected source input: %s", path)}
			} else if !os.IsNotExist(err) {
				return Candidate{}, err
			}
		}
	}
	// Use the selected source's toolchain and Cargo settings. Preserve the
	// host execution/network environment, but remove ambient Rust/Cargo
	// overrides before setting the owned cache locations below.
	var env []string
	for _, value := range os.Environ() {
		if !strings.HasPrefix(value, "RUST") && !strings.HasPrefix(value, "CARGO_") {
			env = append(env, value)
		}
	}
	env = append(env,
		"RUSTUP_HOME="+rustupHome,
		"CARGO_HOME="+cargoHome,
		"CARGO_TARGET_DIR="+cargoTarget,
	)
	// Persisted directory overrides take precedence over rust-toolchain.toml.
	// A shared toolchain cache is usable only when it has no such policy.
	overrides := exec.Command("rustup", "override", "list")
	overrides.Dir = codexRS
	overrides.Env = env
	overrideOut, err := overrides.Output()
	if err != nil {
		return Candidate{}, fmt.Errorf("inspect cached rustup overrides: %w", err)
	}
	if strings.TrimSpace(string(overrideOut)) != "no overrides" {
		return Candidate{}, &Failure{Category: FailureSource, Err: fmt.Errorf("cached rustup directory overrides are not selected source inputs")}
	}
	lockPath := filepath.Join(codexRS, "Cargo.lock")
	originalLock, err := os.ReadFile(lockPath)
	if err != nil {
		return Candidate{}, err
	}
	if err := os.WriteFile(filepath.Join(syncOut, "upstream.Cargo.lock"), originalLock, 0o644); err != nil {
		return Candidate{}, err
	}
	fmt.Fprintf(os.Stderr, "protocolsync upstream Cargo.lock sha256: %x\n", sha256.Sum256(originalLock))
	// --no-deps reads package declarations without resolving dependencies or
	// building upstream code. It provides the real workspace membership.
	metadata := exec.Command("cargo", "metadata", "--no-deps", "--format-version", "1")
	metadata.Dir, metadata.Env = codexRS, env
	metadata.Stderr = os.Stderr
	fmt.Fprintf(os.Stderr, "protocolsync upstream command: %s\n", metadata.String())
	metadataOut, err := metadata.Output()
	if err != nil {
		return Candidate{}, fmt.Errorf("read selected Cargo workspace: %w", err)
	}
	dirty, err := gitOutput(worktree, "status", "--porcelain", "--untracked-files=no")
	if err != nil {
		return Candidate{}, err
	}
	if strings.TrimSpace(dirty) != "" {
		return Candidate{}, &Failure{Category: FailureSource, Err: fmt.Errorf("Cargo metadata changed selected source: %s", dirty)}
	}
	preparedLock, err := prepareLockfile(originalLock, metadataOut, codexRS)
	if err != nil {
		return Candidate{}, &Failure{Category: FailureSource, Err: err}
	}
	if err := os.WriteFile(filepath.Join(syncOut, "prepared.Cargo.lock"), preparedLock, 0o644); err != nil {
		return Candidate{}, err
	}
	fmt.Fprintf(os.Stderr, "protocolsync prepared Cargo.lock sha256: %x\n", sha256.Sum256(preparedLock))
	if err := os.WriteFile(lockPath, preparedLock, 0o644); err != nil {
		return Candidate{}, err
	}
	if err := runCargo(codexRS, env, "run", "--locked", "-p", "codex-cli", "--", "app-server", "generate-json-schema", "--experimental", "--out", schemaDir); err != nil {
		return Candidate{}, err
	}
	if err := runCargo(codexRS, env, "run", "--locked", "-p", "codex-cli", "--", "app-server", "generate-json-schema", "--out", stableDir); err != nil {
		return Candidate{}, err
	}
	versionCmd := exec.Command("cargo", "run", "--locked", "-p", "codex-cli", "--", "--version")
	versionCmd.Dir = codexRS
	versionCmd.Env = env
	fmt.Fprintf(os.Stderr, "protocolsync upstream command: %s\n", versionCmd.String())
	versionOut, err := versionCmd.Output()
	if err != nil {
		return Candidate{}, fmt.Errorf("read exact upstream codex-cli version: %w", err)
	}
	codexVersion := strings.TrimSpace(string(versionOut))
	if !strings.HasPrefix(codexVersion, "codex-cli ") {
		return Candidate{}, fmt.Errorf("unexpected exact upstream codex-cli version %q", codexVersion)
	}
	builtLock, err := os.ReadFile(lockPath)
	if err != nil {
		return Candidate{}, err
	}
	changedSource, err := gitOutput(worktree, "diff", "--name-only", "HEAD")
	if err != nil {
		return Candidate{}, err
	}
	for _, path := range strings.Fields(changedSource) {
		if path != "codex-rs/Cargo.lock" {
			return Candidate{}, &Failure{Category: FailureSource, Err: fmt.Errorf("upstream build changed selected source %s: %s", targetSHA, path)}
		}
	}
	if !bytes.Equal(builtLock, preparedLock) {
		return Candidate{}, &Failure{Category: FailureSource, Err: fmt.Errorf("upstream build changed prepared Cargo.lock for %s", targetSHA)}
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
		GeneratorDetail: filepath.Join(worktree, "codex-rs") + " cargo run --locked -p codex-cli",
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
			return &Failure{Category: FailureSource, Err: fmt.Errorf("cached Codex origin %s does not match %s", strings.TrimSpace(origin), upstreamRepo)}
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
	fmt.Fprintf(os.Stderr, "protocolsync upstream command: %s\n", cmd.String())
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
