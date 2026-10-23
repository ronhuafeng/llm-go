package protocolsync

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// GoalPublicationIntent is written and synced before the one possible push.
// It records an attempted effect, never permission to replay it or acceptance.
// The accompanying bundle contains one credential-free, parentless snapshot.
// Host-local durability cannot survive runner loss before artifact retention.
type GoalPublicationIntent struct {
	Version             int       `json:"version"`
	Repository          string    `json:"repository"`
	Number              int       `json:"number"`
	Branch              string    `json:"branch"`
	BaseBranch          string    `json:"base_branch"`
	BaseSHA             string    `json:"base_sha"`
	VerificationBaseSHA string    `json:"verification_base_sha"`
	AppBotID            int64     `json:"app_bot_id"`
	Target              Target    `json:"target"`
	PreviousHead        string    `json:"previous_head"`
	IntendedHead        string    `json:"intended_head"`
	SnapshotDigest      string    `json:"snapshot_sha256"`
	SnapshotCommit      string    `json:"snapshot_commit"`
	SnapshotTree        string    `json:"snapshot_tree"`
	BundleDigest        string    `json:"bundle_sha256"`
	Deadline            time.Time `json:"deadline"`
}

type GoalPublicationEffect string

const (
	GoalPublicationUnconfirmed GoalPublicationEffect = "unconfirmed"
	GoalPublicationObserved    GoalPublicationEffect = "observed"
)

const goalIntentName = "publication-intent.json"
const goalBundleName = "candidate.bundle"

// ReconcileGoalPublication observes, but never retries, a retained write intent.
// A returned Head is solely an effect locator. Fresh local verification and
// exact-head CI are still required. The previous head does not prove no effect:
// an interrupted push may still finish after this read.
func ReconcileGoalPublication(ctx context.Context, req GoalPublicationRequest) (GoalPublication, error) {
	return reconcileGoalPublication(ctx, req, time.Now())
}

func reconcileGoalPublication(ctx context.Context, req GoalPublicationRequest, now time.Time) (GoalPublication, error) {
	var result GoalPublication
	if err := goalBinding(req); err != nil {
		return result, err
	}
	intent, err := readGoalPublicationIntent(ctx, req)
	if err != nil || intent == nil {
		return result, err
	}
	result = goalIntentResult(intent)
	if !req.Deadline.After(now) {
		return result, errors.New("publication deadline expired; retained intent is not renewed")
	}
	actual, err := goalRemoteHead(ctx, req, req.Branch)
	if err != nil {
		return result, fmt.Errorf("publication outcome unknown; retained intent cannot be replayed: %w", err)
	}
	if actual != intent.IntendedHead {
		if actual == intent.PreviousHead {
			return result, errors.New("publication outcome unresolved at previous head; retained intent cannot be replayed")
		}
		return result, errors.New("publication branch has a foreign head; retained intent cannot be adopted")
	}
	// The remote commit must retain the exact tree and parent recorded before
	// the effect, not merely a matching string in an artifact.
	if _, err = goalGit(ctx, req.RepoRoot, "fetch", "--no-tags", "--no-write-fetch-head", goalRemote(req), "refs/heads/"+req.Branch); err != nil {
		return result, fmt.Errorf("retained publication commit unavailable: %w", err)
	}
	tree, treeErr := goalGit(ctx, req.RepoRoot, "rev-parse", intent.IntendedHead+"^{tree}")
	parents, parentErr := goalGit(ctx, req.RepoRoot, "show", "-s", "--format=%P", intent.IntendedHead)
	if treeErr != nil || parentErr != nil || tree != intent.SnapshotTree || parents != intent.PreviousHead {
		return result, errors.New("retained publication commit differs from intended tree or parent")
	}
	// Validate all live authority, including actor and target, before exposing the
	// intended head as the only safe locator. The receipt M is historical; fresh
	// acceptance must bind current main.
	if err = goalCurrentAuthority(ctx, req, intent.IntendedHead, false); err != nil {
		return result, err
	}
	result.Head = intent.IntendedHead
	result.EffectState = GoalPublicationObserved
	return result, nil
}

func goalIntentResult(intent *GoalPublicationIntent) GoalPublication {
	return GoalPublication{Number: intent.Number, PreviousHead: intent.PreviousHead, SnapshotDigest: intent.SnapshotDigest, Intent: intent, EffectState: GoalPublicationUnconfirmed}
}

func goalPublicationState(req GoalPublicationRequest, create bool) (string, error) {
	if req.StateDir == "" || req.Deadline.IsZero() {
		return "", errors.New("publication requires host state and the original deadline")
	}
	state, err := filepath.Abs(req.StateDir)
	if err != nil {
		return "", errors.New("publication state path unavailable")
	}
	// Never write into a candidate, including via a symlinked parent directory.
	if req.CandidateDir != "" {
		candidate, e := filepath.Abs(req.CandidateDir)
		if e != nil || goalWithin(candidate, state) {
			return "", errors.New("publication state must be outside the candidate")
		}
	}
	if create {
		if err = os.MkdirAll(state, 0700); err != nil {
			return "", errors.New("cannot create host publication state")
		}
	}
	real, err := filepath.EvalSymlinks(state)
	if os.IsNotExist(err) && !create {
		return state, nil
	}
	if err != nil || real != state {
		return "", errors.New("publication state must be a real host-owned directory")
	}
	info, err := os.Stat(state)
	if err != nil || !info.IsDir() || info.Mode().Perm()&0077 != 0 {
		return "", errors.New("publication state directory must be private")
	}
	if req.CandidateDir != "" {
		candidate, e := filepath.EvalSymlinks(req.CandidateDir)
		if e != nil || goalWithin(candidate, real) {
			return "", errors.New("publication state must be outside the candidate")
		}
	}
	return state, nil
}

func readGoalPublicationIntent(ctx context.Context, req GoalPublicationRequest) (*GoalPublicationIntent, error) {
	state, err := goalPublicationState(req, false)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(state, goalIntentName)
	info, err := os.Lstat(path)
	if os.IsNotExist(err) {
		// A bundle without its binding is incomplete state, not a new attempt.
		if _, e := os.Lstat(filepath.Join(state, goalBundleName)); !os.IsNotExist(e) {
			return nil, errors.New("publication intent missing for retained snapshot")
		}
		return nil, nil
	}
	if err != nil || !info.Mode().IsRegular() || info.Size() > 64*1024 || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("publication intent unavailable or unsafe")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, errors.New("publication intent unreadable")
	}
	var intent GoalPublicationIntent
	decoder := json.NewDecoder(strings.NewReader(string(data)))
	decoder.DisallowUnknownFields()
	if err = decoder.Decode(&intent); err != nil || decoder.Decode(new(any)) != io.EOF {
		return nil, errors.New("publication intent incomplete")
	}
	if intent.Version != 1 || intent.Repository != req.Repository || intent.Number != req.Number || intent.Branch != req.Branch || intent.BaseBranch != req.BaseBranch || intent.BaseSHA != req.BaseSHA || intent.AppBotID != req.AppBotID || intent.Target != req.Target || !intent.Deadline.Equal(req.Deadline) || (req.ExpectedHead != intent.PreviousHead && req.ExpectedHead != intent.IntendedHead) {
		return nil, errors.New("publication intent differs from immutable goal binding")
	}
	if !shaRE.MatchString(intent.VerificationBaseSHA) || !shaRE.MatchString(intent.PreviousHead) || !shaRE.MatchString(intent.IntendedHead) || !shaRE.MatchString(intent.SnapshotCommit) || !shaRE.MatchString(intent.SnapshotTree) || !goalDigest(intent.SnapshotDigest) || !goalDigest(intent.BundleDigest) || intent.PreviousHead == intent.IntendedHead || intent.Deadline.IsZero() {
		return nil, errors.New("publication intent incomplete")
	}
	bundle := filepath.Join(state, goalBundleName)
	info, err = os.Lstat(bundle)
	if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0077 != 0 {
		return nil, errors.New("retained publication snapshot missing or unsafe")
	}
	digest, err := goalFileDigest(bundle)
	if err != nil || digest != intent.BundleDigest {
		return nil, errors.New("retained publication snapshot digest changed")
	}
	// Verify both the advertised root and the objects, rather than trusting JSON
	// or a checksum alone. No checkout runs candidate hooks or credential setup.
	work, err := os.MkdirTemp("", "goal-intent-check-")
	if err != nil {
		return nil, errors.New("cannot validate retained publication snapshot")
	}
	defer os.RemoveAll(work)
	if _, err = goalArtifactGit(ctx, work, "init", "--quiet"); err != nil {
		return nil, err
	}
	if _, err = goalArtifactGit(ctx, work, "bundle", "verify", bundle); err != nil {
		return nil, errors.New("retained publication bundle incomplete")
	}
	heads, err := goalArtifactGit(ctx, work, "bundle", "list-heads", bundle)
	if err != nil || heads != intent.SnapshotCommit+" HEAD" {
		return nil, errors.New("retained publication bundle has unexpected refs")
	}
	if _, err = goalArtifactGit(ctx, work, "fetch", "--no-tags", bundle, "HEAD"); err != nil {
		return nil, errors.New("cannot read retained publication snapshot")
	}
	tree, err := goalArtifactGit(ctx, work, "rev-parse", "FETCH_HEAD^{tree}")
	parents, parentErr := goalArtifactGit(ctx, work, "show", "-s", "--format=%P", "FETCH_HEAD")
	if err != nil || parentErr != nil || tree != intent.SnapshotTree || parents != "" {
		return nil, errors.New("retained publication snapshot is not the exact minimal tree")
	}
	if _, err = goalArtifactGit(ctx, work, "checkout", "--quiet", "--detach", "FETCH_HEAD"); err != nil {
		return nil, errors.New("cannot inspect retained publication snapshot")
	}
	digest, err = goalSnapshot(ctx, work, "")
	if err != nil || digest != intent.SnapshotDigest {
		return nil, errors.New("retained publication snapshot does not match verified bytes")
	}
	identity, err := gitBaselineIdentity(work, "HEAD")
	if err != nil || identity.SourceCommit != req.Target.PeeledCommitSHA || identity.SourceRefName != req.Target.RefName || identity.SourceRefKind != req.Target.RefKind {
		return nil, errors.New("retained publication source differs from bound target")
	}
	return &intent, nil
}

func writeGoalPublicationIntent(ctx context.Context, req GoalPublicationRequest, snapshot, head, tree string) (*GoalPublicationIntent, error) {
	state, err := goalPublicationState(req, true)
	if err != nil {
		return nil, err
	}
	if !req.Deadline.After(time.Now()) {
		return nil, errors.New("publication deadline expired")
	}
	commit, err := goalArtifactGit(ctx, snapshot, "rev-parse", "HEAD")
	if err != nil {
		return nil, err
	}
	// A temporary bundle is installed before the new intent. Interrupted writes
	// leave either the old valid pair or incomplete state, which fails closed.
	file, err := os.CreateTemp(state, ".candidate-*.bundle")
	if err != nil {
		return nil, errors.New("cannot retain publication snapshot")
	}
	bundle := file.Name()
	_ = file.Close()
	defer os.Remove(bundle)
	if _, err = goalArtifactGit(ctx, snapshot, "bundle", "create", bundle, "HEAD"); err != nil {
		return nil, err
	}
	if err = os.Chmod(bundle, 0600); err != nil {
		return nil, errors.New("cannot protect publication snapshot")
	}
	if err = goalSyncFile(bundle); err != nil {
		return nil, errors.New("cannot sync publication snapshot")
	}
	digest, err := goalFileDigest(bundle)
	if err != nil {
		return nil, errors.New("cannot hash publication snapshot")
	}
	intent := &GoalPublicationIntent{Version: 1, Repository: req.Repository, Number: req.Number, Branch: req.Branch, BaseBranch: req.BaseBranch, BaseSHA: req.BaseSHA, VerificationBaseSHA: goalVerificationBase(req), AppBotID: req.AppBotID, Target: req.Target, PreviousHead: req.ExpectedHead, IntendedHead: head, SnapshotDigest: req.SnapshotDigest, SnapshotCommit: commit, SnapshotTree: tree, BundleDigest: digest, Deadline: req.Deadline}
	data, err := json.MarshalIndent(intent, "", "  ")
	if err != nil {
		return nil, errors.New("cannot encode publication intent")
	}
	if err = os.Rename(bundle, filepath.Join(state, goalBundleName)); err != nil {
		return nil, errors.New("cannot retain publication snapshot")
	}
	file, err = os.CreateTemp(state, ".publication-intent-*.json")
	if err != nil {
		return nil, errors.New("cannot retain publication intent")
	}
	tmp := file.Name()
	defer os.Remove(tmp)
	_, err = file.Write(append(data, '\n'))
	if err == nil {
		err = file.Sync()
	}
	closeErr := file.Close()
	if err != nil || closeErr != nil {
		return nil, errors.New("cannot sync publication intent")
	}
	if err = os.Rename(tmp, filepath.Join(state, goalIntentName)); err != nil {
		return nil, errors.New("cannot retain publication intent")
	}
	dir, err := os.Open(state)
	if err != nil {
		return nil, errors.New("cannot sync publication state")
	}
	err = dir.Sync()
	closeErr = dir.Close()
	if err != nil || closeErr != nil {
		return nil, errors.New("cannot sync publication state")
	}
	return intent, nil
}

func goalDigest(value string) bool {
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == sha256.Size && value == strings.ToLower(value)
}

func goalFileDigest(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()
	hash := sha256.New()
	if _, err = io.Copy(hash, file); err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", hash.Sum(nil)), nil
}

func goalSyncFile(path string) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	err = file.Sync()
	closeErr := file.Close()
	if err != nil {
		return err
	}
	return closeErr
}

// Artifact inspection must not consult host filters, templates, credentials or
// candidate hooks. It imports only the fixed local bundle and never networks.
func goalArtifactGit(ctx context.Context, root string, args ...string) (string, error) {
	fixed := []string{"-c", "core.hooksPath=/dev/null", "-c", "core.fsmonitor=false", "-C", root}
	cmd := exec.CommandContext(ctx, "git", append(fixed, args...)...)
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GIT_") && !strings.HasPrefix(entry, "GH_TOKEN=") && !strings.HasPrefix(entry, "GITHUB_TOKEN=") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null", "GIT_TEMPLATE_DIR=")
	out, err := cmd.Output()
	if err != nil {
		return "", safeGoalPublicationError("git", err)
	}
	return strings.TrimSpace(string(out)), nil
}
