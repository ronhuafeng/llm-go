package protocolsync

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const verifierWorkflow = ".github/workflows/codexsdk-upstream-protocol-sync.yml"
const verifierBranchPrefix = "codex/verifier-repair/"
const verifierArchiveLimit = 128 << 20
const verifierExpandedLimit = 256 << 20

// VerifierHandoff is written only by trusted control after executed evidence and
// independent review. A PR description can locate its origin but cannot supply
// any of these authority-bearing fields.
type VerifierHandoff struct {
	DeadlineUTC         string `json:"deadline_utc"`
	Version             int    `json:"version"`
	OriginRunID         int64  `json:"origin_run_id"`
	OriginAttempt       int    `json:"origin_attempt"`
	BaseSHA             string `json:"base_sha"`
	Target              Target `json:"target"`
	CandidateSHA256     string `json:"candidate_sha256"`
	ProposalSHA256      string `json:"proposal_sha256"`
	VerifierPatchSHA256 string `json:"verifier_patch_sha256"`
	ProofSHA256         string `json:"proof_sha256"`
	ReviewSHA256        string `json:"review_sha256"`
	ReviewedTree        string `json:"reviewed_tree"`
	RoundsUsed          int    `json:"rounds_used"`
	MaxRounds           int    `json:"max_rounds"`
	Handoffs            int    `json:"handoffs"`
}

// VerifierArtifactLoader returns the raw GitHub artifact ZIP, never arbitrary
// extracted files. The native artifact digest is checked before parsing it.
type VerifierArtifactLoader func(artifactID int64) ([]byte, error)

type VerifierPublicationRequest struct {
	// ReadAPI is optional separate read authority. Native defaults use
	// GH_READ_TOKEN for GitHub GET/artifact download, while effects retain GH_TOKEN.
	ReadAPI                                  PublicationAPI
	RepoRoot, Repository, BaseBranch, Remote string
	AppBotID                                 int64
	OriginRunID                              int64
	OriginAttempt                            int
	// The only incomplete origin allowed is the current trusted publishing run.
	CurrentRunID int64
	API          PublicationAPI
	LoadArtifact VerifierArtifactLoader
	Lookuper     RemoteLookuper
}

type VerifierResolveRequest struct {
	VerifierPublicationRequest
	Number       int
	ExpectedHead string
	// Merged resolution additionally requires native merged state and containment
	// in current main. Ordinary resolution admits only an open proposal.
	Merged bool
}

type VerifierPublication struct {
	Number         int             `json:"number"`
	URL            string          `json:"url"`
	Branch         string          `json:"branch"`
	Head           string          `json:"head"`
	Handoff        VerifierHandoff `json:"handoff"`
	ArtifactID     int64           `json:"artifact_id"`
	ArtifactDigest string          `json:"artifact_digest"`
}

type VerifierContinuationRequest struct {
	Now func() time.Time
	VerifierResolveRequest
	// This entry is called only in the existing serialized sync workflow. The
	// current run is excluded from native prior-run observation.
	ContinuationRunID int64
	// Optional empty destination receives the already digest-verified checkpoint.
	OutputDir string
}

type VerifierContinuation struct {
	Publication          VerifierPublication `json:"publication"`
	MainSHA              string              `json:"main_sha"`
	RemainingRounds      int                 `json:"remaining_rounds"`
	Duplicate            bool                `json:"duplicate"`
	CheckpointArtifactID int64               `json:"checkpoint_artifact_id"`
	CheckpointDigest     string              `json:"checkpoint_digest"`
	CandidateDir         string              `json:"candidate_dir,omitempty"`
	ProposalPatchPath    string              `json:"proposal_patch_path,omitempty"`
}

type verifierNativeRun struct {
	ID           int64  `json:"id"`
	RunAttempt   int    `json:"run_attempt"`
	Path         string `json:"path"`
	Event        string `json:"event"`
	HeadSHA      string `json:"head_sha"`
	HeadBranch   string `json:"head_branch"`
	Status       string `json:"status"`
	Conclusion   string `json:"conclusion"`
	DisplayTitle string `json:"display_title"`
	Repository   struct {
		FullName string `json:"full_name"`
	} `json:"repository"`
	HeadRepository struct {
		FullName string `json:"full_name"`
	} `json:"head_repository"`
	PullRequests []struct {
		Number int `json:"number"`
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
	} `json:"pull_requests"`
}

type verifierArtifact struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Digest      string `json:"digest"`
	Expired     bool   `json:"expired"`
	WorkflowRun struct {
		ID      int64  `json:"id"`
		HeadSHA string `json:"head_sha"`
	} `json:"workflow_run"`
}

type verifierBundle struct {
	handoff  VerifierHandoff
	artifact verifierArtifact
	files    map[string][]byte
}

var verifierDigestRE = regexp.MustCompile(`^[0-9a-f]{64}$`)
var verifierRepositoryRE = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

func verifierRequest(req VerifierPublicationRequest) (VerifierPublicationRequest, error) {
	if req.RepoRoot == "" || !verifierRepositoryRE.MatchString(req.Repository) || req.BaseBranch == "" || req.AppBotID <= 0 {
		return req, publicationPolicy("verifier publication requires repository, base, and publishing App identity")
	}
	if req.Remote == "" {
		req.Remote = "origin"
	}
	suppliedAPI := req.API
	if req.API == nil {
		req.API = githubPublicationAPI{repoRoot: req.RepoRoot}
	}
	if req.ReadAPI == nil {
		if suppliedAPI != nil {
			req.ReadAPI = suppliedAPI
		} else {
			req.ReadAPI = verifierReadAPI{repoRoot: req.RepoRoot}
		}
	}
	req.API = verifierRoutedAPI{read: req.ReadAPI, write: req.API}
	if req.LoadArtifact == nil {
		req.LoadArtifact = func(id int64) ([]byte, error) {
			command := exec.Command("gh", "api", fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", req.Repository, id))
			command.Dir = req.RepoRoot
			command.Env = verifierReadEnvironment()
			var stderr bytes.Buffer
			command.Stderr = &stderr
			stdout, err := command.StdoutPipe()
			if err != nil {
				return nil, err
			}
			if err = command.Start(); err != nil {
				return nil, err
			}
			raw, readErr := io.ReadAll(io.LimitReader(stdout, verifierArchiveLimit+1))
			if len(raw) > verifierArchiveLimit {
				_ = command.Process.Kill()
			}
			waitErr := command.Wait()
			if readErr != nil {
				return nil, readErr
			}
			if len(raw) > verifierArchiveLimit {
				return nil, publicationPolicy("verifier archive too large")
			}
			if waitErr != nil {
				return nil, fmt.Errorf("download verifier artifact: %w: %s", waitErr, stderr.String())
			}
			return raw, nil
		}
	}
	return req, nil
}

// GitHub may qualify the native workflow path with @ref. The file identity is
// exact; native repository, head SHA, event and origin branch separately bind
// the source revision, so a ref suffix never substitutes for those checks.
func verifierWorkflowPath(value, expected string) bool {
	file, ref, qualified := strings.Cut(value, "@")
	return file == expected && (!qualified || ref != "")
}

func readVerifierRun(req VerifierPublicationRequest, id int64) (verifierNativeRun, error) {
	var run verifierNativeRun
	if id <= 0 {
		return run, publicationPolicy("verifier origin run is missing")
	}
	if err := req.API.Request("GET", fmt.Sprintf("repos/%s/actions/runs/%d", req.Repository, id), nil, &run); err != nil {
		return run, err
	}
	if run.ID != id || !verifierWorkflowPath(run.Path, verifierWorkflow) || run.Repository.FullName != req.Repository || run.HeadRepository.FullName != req.Repository || !shaRE.MatchString(run.HeadSHA) || run.RunAttempt < 1 {
		return run, publicationPolicy("verifier origin is not the trusted same-repository main workflow")
	}
	return run, nil
}

func findVerifierArtifact(req VerifierPublicationRequest, run verifierNativeRun, name string) (verifierArtifact, error) {
	var found []verifierArtifact
	count, total := 0, -1
	for page := 1; page <= 100; page++ {
		var batch struct {
			Total     int                `json:"total_count"`
			Artifacts []verifierArtifact `json:"artifacts"`
		}
		if err := req.API.Request("GET", fmt.Sprintf("repos/%s/actions/runs/%d/artifacts?per_page=100&page=%d", req.Repository, run.ID, page), nil, &batch); err != nil {
			return verifierArtifact{}, err
		}
		if total < 0 {
			total = batch.Total
		} else if total != batch.Total {
			return verifierArtifact{}, publicationPolicy("artifact listing changed")
		}
		count += len(batch.Artifacts)
		for _, a := range batch.Artifacts {
			if a.Name == name {
				found = append(found, a)
			}
		}
		if len(batch.Artifacts) < 100 {
			if count != total || len(found) != 1 {
				return verifierArtifact{}, publicationPolicy("expected one complete native artifact %s", name)
			}
			a := found[0]
			if a.ID <= 0 || a.Expired || a.WorkflowRun.ID != run.ID || a.WorkflowRun.HeadSHA != run.HeadSHA || !strings.HasPrefix(a.Digest, "sha256:") || !verifierDigestRE.MatchString(strings.TrimPrefix(a.Digest, "sha256:")) {
				return verifierArtifact{}, publicationPolicy("artifact origin or digest is invalid")
			}
			return a, nil
		}
	}
	return verifierArtifact{}, publicationPolicy("artifact listing exceeds bound")
}

func loadVerifierArchive(req VerifierPublicationRequest, artifact verifierArtifact) (map[string][]byte, error) {
	raw, err := req.LoadArtifact(artifact.ID)
	if err != nil {
		return nil, err
	}
	if len(raw) > verifierArchiveLimit || "sha256:"+repairDigest(raw) != artifact.Digest {
		return nil, publicationPolicy("native artifact archive digest mismatch")
	}
	reader, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, err
	}
	if len(reader.File) > 20000 {
		return nil, publicationPolicy("artifact has too many entries")
	}
	files := map[string][]byte{}
	seen := map[string]bool{}
	total := 0
	for _, f := range reader.File {
		name := strings.TrimSuffix(f.Name, "/")
		if name == "" || path.Clean(name) != name || path.IsAbs(name) || strings.Contains(name, "\\") || name == ".." || strings.HasPrefix(name, "../") || seen[name] {
			return nil, publicationPolicy("unsafe or duplicate artifact path")
		}
		seen[name] = true
		if f.FileInfo().IsDir() {
			continue
		}
		if !f.Mode().IsRegular() {
			return nil, publicationPolicy("artifact contains a non-regular file")
		}
		if f.UncompressedSize64 > verifierExpandedLimit {
			return nil, publicationPolicy("expanded artifact exceeds bound")
		}
		opened, e := f.Open()
		if e != nil {
			return nil, e
		}
		data, e := io.ReadAll(io.LimitReader(opened, int64(verifierExpandedLimit-total+1)))
		closeErr := opened.Close()
		if e != nil {
			return nil, e
		}
		if closeErr != nil {
			return nil, closeErr
		}
		total += len(data)
		if total > verifierExpandedLimit {
			return nil, publicationPolicy("expanded artifact exceeds bound")
		}
		files[name] = data
	}
	return files, nil
}

func loadVerifierBundle(req VerifierPublicationRequest) (verifierBundle, error) {
	var out verifierBundle
	run, err := readVerifierRun(req, req.OriginRunID)
	if err != nil {
		return out, err
	}
	if run.HeadBranch != req.BaseBranch || run.RunAttempt != req.OriginAttempt || (run.Event != "workflow_dispatch" && run.Event != "schedule") {
		return out, publicationPolicy("verifier origin attempt/event mismatch")
	}
	if run.Status != "completed" && !(run.ID == req.CurrentRunID && run.Status == "in_progress") {
		return out, publicationPolicy("verifier origin has no completed artifact-producing attempt")
	}
	if run.Status == "completed" && run.Conclusion != "success" && run.Conclusion != "failure" {
		return out, publicationPolicy("cancelled or unknown verifier origin cannot authorize publication")
	}
	artifact, err := findVerifierArtifact(req, run, fmt.Sprintf("protocol-sync-verifier-proposal-%d", req.OriginAttempt))
	if err != nil {
		return out, err
	}
	files, err := loadVerifierArchive(req, artifact)
	if err != nil {
		return out, err
	}
	if len(files) != 5 {
		return out, publicationPolicy("verifier artifact must contain exactly handoff.json, verifier.patch, proof.json, review.json, cases.json")
	}
	for _, name := range []string{"handoff.json", "verifier.patch", "proof.json", "review.json", "cases.json"} {
		if len(files[name]) == 0 {
			return out, publicationPolicy("verifier artifact missing %s", name)
		}
	}
	var h VerifierHandoff
	if err = json.Unmarshal(files["handoff.json"], &h); err != nil {
		return out, err
	}
	if h.Version != 1 || h.OriginRunID != run.ID || h.OriginAttempt != run.RunAttempt || h.BaseSHA != run.HeadSHA || !shaRE.MatchString(h.ReviewedTree) || h.Target.RefName == "" || !validKinds[h.Target.RefKind] || !shaRE.MatchString(h.Target.PeeledCommitSHA) || h.Handoffs != 1 || h.MaxRounds < 1 || h.MaxRounds > 12 || h.RoundsUsed < 1 || h.RoundsUsed > h.MaxRounds {
		return out, publicationPolicy("verifier handoff identity or remaining budget is invalid")
	}
	if deadline, e := time.Parse(time.RFC3339, h.DeadlineUTC); e != nil || deadline.IsZero() {
		return out, publicationPolicy("verifier handoff has no trusted original deadline")
	}
	for _, digest := range []string{h.CandidateSHA256, h.ProposalSHA256, h.VerifierPatchSHA256, h.ProofSHA256, h.ReviewSHA256} {
		if !verifierDigestRE.MatchString(digest) {
			return out, publicationPolicy("verifier handoff digest is invalid")
		}
	}
	if repairDigest(files["verifier.patch"]) != h.VerifierPatchSHA256 || repairDigest(files["proof.json"]) != h.ProofSHA256 || repairDigest(files["review.json"]) != h.ReviewSHA256 {
		return out, publicationPolicy("verifier evidence bytes changed")
	}
	if err = ValidateVerifierEvidence(h, files["verifier.patch"], files["proof.json"], files["review.json"], files["cases.json"]); err != nil {
		return out, err
	}
	out = verifierBundle{handoff: h, artifact: artifact, files: files}
	return out, nil
}

// VerifierReviewEvidence identities and digests are supplied by trusted control,
// not by the model's review response. Approval alone is never provenance.
type VerifierReviewEvidence struct {
	Approved            bool   `json:"approved"`
	AuthorSessionID     string `json:"author_session_id"`
	ReviewerSessionID   string `json:"reviewer_session_id"`
	CandidateSHA256     string `json:"candidate_sha256"`
	ProposalSHA256      string `json:"proposal_sha256"`
	VerifierPatchSHA256 string `json:"verifier_patch_sha256"`
	ProofSHA256         string `json:"proof_sha256"`
	ReviewedTree        string `json:"reviewed_tree"`
	Reason              string `json:"reason"`
}

func validateVerifierReview(bundle verifierBundle) error {
	var review VerifierReviewEvidence
	if err := json.Unmarshal(bundle.files["review.json"], &review); err != nil {
		return err
	}
	h := bundle.handoff
	if !review.Approved || review.AuthorSessionID == "" || review.ReviewerSessionID == "" || review.AuthorSessionID == review.ReviewerSessionID || strings.TrimSpace(review.Reason) == "" || review.CandidateSHA256 != h.CandidateSHA256 || review.ProposalSHA256 != h.ProposalSHA256 || review.VerifierPatchSHA256 != h.VerifierPatchSHA256 || review.ProofSHA256 != h.ProofSHA256 || review.ReviewedTree != h.ReviewedTree {
		return publicationPolicy("verifier review is refused, unknown, self-authored, or bound to different bytes")
	}
	return nil
}

func verifierBranch(h VerifierHandoff) string {
	return fmt.Sprintf("%s%d-%d-%s", verifierBranchPrefix, h.OriginRunID, h.OriginAttempt, h.VerifierPatchSHA256[:12])
}
func verifierLocator(h VerifierHandoff) string {
	return fmt.Sprintf("<!-- protocol-sync-verifier origin_run_id:%d origin_attempt:%d -->", h.OriginRunID, h.OriginAttempt)
}

var verifierLocatorRE = regexp.MustCompile(`<!-- protocol-sync-verifier origin_run_id:([1-9][0-9]*) origin_attempt:([1-9][0-9]*) -->`)

func parseVerifierLocator(body string) (int64, int, error) {
	matches := verifierLocatorRE.FindAllStringSubmatch(body, -1)
	if len(matches) != 1 {
		return 0, 0, publicationPolicy("verifier PR has no unambiguous origin locator")
	}
	run, err := strconv.ParseInt(matches[0][1], 10, 64)
	if err != nil {
		return 0, 0, err
	}
	attempt, err := strconv.Atoi(matches[0][2])
	return run, attempt, err
}

func verifyVerifierTree(req VerifierPublicationRequest, bundle verifierBundle, head string) error {
	h := bundle.handoff
	if !shaRE.MatchString(head) {
		return publicationPolicy("verifier head is not exact")
	}
	inspection := PendingRequest{RepoRoot: req.RepoRoot, Remote: req.Remote}
	for _, sha := range []string{h.BaseSHA, head} {
		if err := fetchPublicationCommit(inspection, sha); err != nil {
			return err
		}
	}
	parents, err := gitOutput(req.RepoRoot, "rev-list", "--parents", "-n", "1", head)
	if err != nil {
		return err
	}
	fields := strings.Fields(parents)
	if len(fields) != 2 || fields[1] != h.BaseSHA {
		return publicationPolicy("verifier PR must be one commit on its reviewed accepted base")
	}
	tree, err := gitOutput(req.RepoRoot, "rev-parse", head+"^{tree}")
	if err != nil {
		return err
	}
	if strings.TrimSpace(tree) != h.ReviewedTree {
		return publicationPolicy("verifier head differs from reviewed tree")
	}
	names, err := gitNUL(req.RepoRoot, "diff", "--name-only", "--no-renames", "-z", h.BaseSHA, head, "--")
	if err != nil {
		return err
	}
	if len(names) == 0 {
		return publicationPolicy("empty verifier proposal")
	}
	if err = validatePaths(names, "verifier"); err != nil {
		return err
	}
	for _, name := range names {
		raw, e := gitOutput(req.RepoRoot, "ls-tree", head, "--", name)
		if e != nil {
			return e
		}
		// Deletions are in the fixed path scope; existing files must be ordinary Go.
		if raw != "" && !strings.HasPrefix(raw, "100644 blob ") {
			return publicationPolicy("verifier proposal contains a non-regular source file")
		}
	}
	// Diff encoding is not identity. Apply the bound bytes to a disposable base
	// index, then require that exact Git tree instead of reserializing the patch.
	treeFromPatch, err := verifierPatchTree(req.RepoRoot, h.BaseSHA, bundle.files["verifier.patch"])
	if err != nil {
		return err
	}
	if treeFromPatch != h.ReviewedTree {
		return publicationPolicy("verifier patch does not reproduce the reviewed exact-head tree")
	}
	return nil
}

func verifierPatchTree(root, base string, patch []byte) (string, error) {
	parent, err := os.MkdirTemp("", "verifier-publication-tree-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(parent)
	sandbox := filepath.Join(parent, "repo")
	if err = runGit(root, "clone", "--quiet", "--no-hardlinks", root, sandbox); err != nil {
		return "", err
	}
	if err = runGit(sandbox, "checkout", "--quiet", "--detach", base); err != nil {
		return "", err
	}
	command := exec.Command("git", "-C", sandbox, "apply", "--cached", "--whitespace=nowarn", "-")
	command.Stdin = bytes.NewReader(patch)
	if raw, e := command.CombinedOutput(); e != nil {
		return "", fmt.Errorf("verifier patch cannot reproduce base tree: %w: %s", e, raw)
	}
	names, err := gitNUL(sandbox, "diff", "--cached", "--name-only", "--no-renames", "-z", base, "--")
	if err != nil {
		return "", err
	}
	if len(names) == 0 {
		return "", publicationPolicy("empty verifier proposal")
	}
	if err = validatePaths(names, "verifier"); err != nil {
		return "", err
	}
	for _, name := range names {
		entry, e := gitOutput(sandbox, "ls-files", "--stage", "--", name)
		if e != nil {
			return "", e
		}
		if entry != "" && !strings.HasPrefix(entry, "100644 ") {
			return "", publicationPolicy("verifier patch contains a non-regular source file")
		}
	}
	tree, err := gitOutput(sandbox, "write-tree")
	return strings.TrimSpace(tree), err
}

func freshVerifierTarget(req VerifierPublicationRequest, h VerifierHandoff) error {
	target, err := ResolveUpstream(ResolveRequest{UpstreamRef: h.Target.RefName, Lookuper: req.Lookuper})
	if err != nil {
		return err
	}
	if target.RefName != h.Target.RefName || target.RefKind != h.Target.RefKind || target.PeeledCommitSHA != h.Target.PeeledCommitSHA {
		return &Failure{Category: FailureSource, Err: fmt.Errorf("verifier continuation upstream target changed")}
	}
	return nil
}

// PublishVerifier publishes only a prepared verifier commit. It does not invoke
// the ordinary sync publisher, rewrite baseline identity, repair a branch, or
// modify any sync PR/tombstone. All ambiguous writes are resolved by readback.
func PublishVerifier(request VerifierPublicationRequest) (VerifierPublication, error) {
	var result VerifierPublication
	req, err := verifierRequest(request)
	if err != nil {
		return result, err
	}
	if err = AssertClean(req.RepoRoot); err != nil {
		return result, err
	}
	bundle, err := loadVerifierBundle(req)
	if err != nil {
		return result, err
	}
	if err = validateVerifierReview(bundle); err != nil {
		return result, err
	}
	if _, _, err = loadVerifierCheckpoint(req, bundle.handoff); err != nil {
		return result, err
	}
	if err = freshVerifierTarget(req, bundle.handoff); err != nil {
		return result, err
	}
	head, err := gitOutput(req.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return result, err
	}
	head = strings.TrimSpace(head)
	if err = verifyVerifierTree(req, bundle, head); err != nil {
		return result, err
	}
	base, err := remotePublicationHead(req.RepoRoot, req.Remote, req.BaseBranch)
	if err != nil {
		return result, err
	}
	if base != bundle.handoff.BaseSHA {
		return result, publicationPolicy("main changed before verifier publication")
	}
	branch := verifierBranch(bundle.handoff)
	all, err := listPublicationPRs(req.API, req.Repository)
	if err != nil {
		return result, err
	}
	var existing *publicationPR
	for _, pr := range all {
		origin, _, _ := parseVerifierLocator(pr.Body)
		sameOriginBranch := strings.HasPrefix(pr.Head.Ref, fmt.Sprintf("%s%d-", verifierBranchPrefix, req.OriginRunID))
		sameNativeHead := pr.Head.Repo.FullName == req.Repository && pr.Head.SHA == head
		if pr.Head.Ref != branch && origin != req.OriginRunID && !sameOriginBranch && !sameNativeHead {
			continue
		}
		if existing != nil || pr.Head.Ref != branch || pr.State != "open" || pr.MergedAt != nil {
			return result, publicationPolicy("verifier origin already has a closed, merged, different, or ambiguous handoff")
		}
		copy := pr
		existing = &copy
	}
	actual, err := remotePublicationHead(req.RepoRoot, req.Remote, branch)
	if err != nil {
		return result, err
	}
	if actual != "" {
		if err = verifyVerifierTree(req, bundle, actual); err != nil {
			return result, err
		}
		actor, e := publicationHeadActor(PendingRequest{RepoRoot: req.RepoRoot, Repository: req.Repository}, req.API, branch, actual)
		if e != nil {
			return result, e
		}
		if e = verifyAppActor(actor, req.AppBotID); e != nil {
			return result, e
		}
		head = actual
	} else {
		if existing != nil {
			return result, publicationPolicy("existing verifier PR lost its head branch")
		}
		// An empty expected-old lease is creation-only. It cannot replace an
		// existing ref, even if another writer creates one after our absence read.
		pushErr := runGit(req.RepoRoot, "push", "--force-with-lease=refs/heads/"+branch+":", req.Remote, head+":refs/heads/"+branch)
		actual, err = remotePublicationHead(req.RepoRoot, req.Remote, branch)
		if err != nil {
			return result, fmt.Errorf("verifier push outcome unknown (%v): %w", pushErr, err)
		}
		if actual != head {
			return result, fmt.Errorf("verifier push did not establish exact head: %v", pushErr)
		}
	}
	if existing == nil {
		body := "Reviewed verifier-only proposal. The original protocol upgrade remains unaccepted.\n\n" + verifierLocator(bundle.handoff)
		writeErr := req.API.Request("POST", "repos/"+req.Repository+"/pulls", map[string]string{"head": branch, "base": req.BaseBranch, "title": "fix(codexsdk): reviewed protocol verifier repair", "body": body}, nil)
		all, err = listPublicationPRs(req.API, req.Repository)
		if err != nil {
			return result, fmt.Errorf("verifier PR creation unknown (%v): %w", writeErr, err)
		}
		for _, pr := range all {
			if pr.Head.Ref == branch && pr.State == "open" {
				if existing != nil {
					return result, publicationPolicy("ambiguous verifier PR readback")
				}
				copy := pr
				existing = &copy
			}
		}
		if existing == nil {
			return result, fmt.Errorf("verifier PR creation unconfirmed: %v", writeErr)
		}
	}
	return ResolveVerifier(VerifierResolveRequest{VerifierPublicationRequest: req, Number: existing.Number, ExpectedHead: head})
}

// ResolveVerifier is a read-only native authority check. A PR body is used only
// to locate the original native workflow artifact; every fact is revalidated.
func ResolveVerifier(request VerifierResolveRequest) (VerifierPublication, error) {
	var result VerifierPublication
	req, err := verifierRequest(request.VerifierPublicationRequest)
	if err != nil {
		return result, err
	}
	if request.Number <= 0 || !shaRE.MatchString(request.ExpectedHead) {
		return result, publicationPolicy("verifier resolution requires exact PR number and expected H")
	}
	var pr publicationPR
	if err = req.API.Request("GET", fmt.Sprintf("repos/%s/pulls/%d", req.Repository, request.Number), nil, &pr); err != nil {
		return result, err
	}
	if pr.Number != request.Number || pr.Base.Ref != req.BaseBranch || pr.Head.Repo.FullName != req.Repository || !strings.HasPrefix(pr.Head.Ref, verifierBranchPrefix) {
		return result, publicationPolicy("verifier PR identity mismatch")
	}
	if err = verifyAppActor(pr.User, req.AppBotID); err != nil {
		return result, err
	}
	origin, attempt, err := parseVerifierLocator(pr.Body)
	if err != nil {
		return result, err
	}
	if req.OriginRunID != 0 && req.OriginRunID != origin || req.OriginAttempt != 0 && req.OriginAttempt != attempt {
		return result, publicationPolicy("verifier PR origin locator changed")
	}
	req.OriginRunID, req.OriginAttempt = origin, attempt
	bundle, err := loadVerifierBundle(req)
	if err != nil {
		return result, err
	}
	if err = validateVerifierReview(bundle); err != nil {
		return result, err
	}
	if _, _, err = loadVerifierCheckpoint(req, bundle.handoff); err != nil {
		return result, err
	}
	if pr.Head.Ref != verifierBranch(bundle.handoff) {
		return result, publicationPolicy("verifier branch is not bound to native origin")
	}
	head := request.ExpectedHead
	if request.Merged {
		if pr.State != "closed" || pr.MergedAt == nil || pr.Head.SHA != head || pr.MergeSHA != head {
			return result, publicationPolicy("verifier PR was not integrated as its exact original head")
		}
	} else {
		if pr.State != "open" || pr.MergedAt != nil {
			return result, publicationPolicy("verifier PR is not open")
		}
		actual, e := remotePublicationHead(req.RepoRoot, req.Remote, pr.Head.Ref)
		if e != nil {
			return result, e
		}
		if actual != head {
			return result, publicationPolicy("verifier PR head changed")
		}
		actor, e := publicationHeadActor(PendingRequest{RepoRoot: req.RepoRoot, Repository: req.Repository}, req.API, pr.Head.Ref, head)
		if e != nil {
			return result, e
		}
		if e = verifyAppActor(actor, req.AppBotID); e != nil {
			return result, e
		}
	}
	if err = verifyVerifierTree(req, bundle, head); err != nil {
		return result, err
	}
	main, err := remotePublicationHead(req.RepoRoot, req.Remote, req.BaseBranch)
	if err != nil {
		return result, err
	}
	expectedMain := bundle.handoff.BaseSHA
	if request.Merged {
		expectedMain = head
	}
	if main != expectedMain {
		return result, publicationPolicy("verifier accepted main changed; reconstruct instead of reusing stale evidence")
	}
	if err = freshVerifierTarget(req, bundle.handoff); err != nil {
		return result, err
	}
	return VerifierPublication{Number: pr.Number, URL: pr.URL, Branch: pr.Head.Ref, Head: head, Handoff: bundle.handoff, ArtifactID: bundle.artifact.ID, ArtifactDigest: bundle.artifact.Digest}, nil
}

// ValidateVerifierEvidence checks byte/identity binding of trusted runner and
// reviewer output. It does not turn caller-supplied JSON into authority: native
// workflow/artifact provenance is independently required at every consumer.
func ValidateVerifierEvidence(h VerifierHandoff, patch, proofJSON, reviewJSON, casesJSON []byte) error {
	if repairDigest(patch) != h.VerifierPatchSHA256 || repairDigest(proofJSON) != h.ProofSHA256 || repairDigest(reviewJSON) != h.ReviewSHA256 {
		return publicationPolicy("verifier evidence digest mismatch")
	}
	var proof struct {
		Passed              bool              `json:"passed"`
		CandidateSHA256     string            `json:"candidate_sha256"`
		ProposalSHA256      string            `json:"proposal_sha256"`
		VerifierPatchSHA256 string            `json:"verifier_patch_sha256"`
		CasesSHA256         string            `json:"cases_sha256"`
		SourceSHA256        string            `json:"source_sha256"`
		Lanes               []json.RawMessage `json:"lanes"`
	}
	if err := json.Unmarshal(proofJSON, &proof); err != nil {
		return err
	}
	if !proof.Passed || len(proof.Lanes) == 0 || proof.CandidateSHA256 != h.CandidateSHA256 || proof.ProposalSHA256 != h.ProposalSHA256 || proof.VerifierPatchSHA256 != h.VerifierPatchSHA256 || proof.CasesSHA256 != repairDigest(casesJSON) || !verifierDigestRE.MatchString(proof.SourceSHA256) {
		return publicationPolicy("verifier executed proof is missing, failed, or bound to different inputs")
	}
	return validateVerifierReview(verifierBundle{handoff: h, files: map[string][]byte{"review.json": reviewJSON}})
}

func writeVerifierFiles(dir string, files map[string][]byte) error {
	if err := os.Mkdir(dir, 0700); err != nil {
		return fmt.Errorf("verifier output must be a fresh directory: %w", err)
	}
	for name, data := range files {
		dst := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
			return err
		}
		file, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			return err
		}
		_, writeErr := file.Write(data)
		closeErr := file.Close()
		if writeErr != nil {
			return writeErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

func loadVerifierCheckpoint(req VerifierPublicationRequest, h VerifierHandoff) (verifierArtifact, map[string][]byte, error) {
	run, err := readVerifierRun(req, h.OriginRunID)
	if err != nil {
		return verifierArtifact{}, nil, err
	}
	if run.RunAttempt != h.OriginAttempt || run.HeadSHA != h.BaseSHA || run.HeadBranch != req.BaseBranch {
		return verifierArtifact{}, nil, publicationPolicy("checkpoint origin changed")
	}
	artifact, err := findVerifierArtifact(req, run, fmt.Sprintf("protocol-sync-repair-checkpoint-%d", h.OriginAttempt))
	if err != nil {
		return artifact, nil, err
	}
	files, err := loadVerifierArchive(req, artifact)
	if err != nil {
		return artifact, nil, err
	}
	var cp struct {
		Version          int    `json:"version"`
		Status           string `json:"status"`
		DeadlineUTC      string `json:"deadline_utc"`
		Base             string `json:"base"`
		Target           Target `json:"target"`
		CandidateSHA256  string `json:"candidate_sha256"`
		ProposalSHA256   string `json:"proposal_sha256"`
		Round            int    `json:"round"`
		MaxRounds        int    `json:"max_rounds"`
		VerifierHandoffs int    `json:"verifier_handoffs"`
	}
	if err = json.Unmarshal(files["checkpoint.json"], &cp); err != nil {
		return artifact, nil, err
	}
	if cp.Version != 1 || cp.Status != "verifier_proposed" || cp.DeadlineUTC != h.DeadlineUTC || cp.Base != h.BaseSHA || cp.Target.RefName != h.Target.RefName || cp.Target.RefKind != h.Target.RefKind || cp.Target.PeeledCommitSHA != h.Target.PeeledCommitSHA || cp.CandidateSHA256 != h.CandidateSHA256 || cp.ProposalSHA256 != h.ProposalSHA256 || cp.Round != h.RoundsUsed || cp.MaxRounds != h.MaxRounds || cp.VerifierHandoffs != 1 || repairDigest(files["proposal.patch"]) != h.ProposalSHA256 {
		return artifact, nil, publicationPolicy("original repair checkpoint identity or consumed budget mismatch")
	}
	for name := range files {
		if name != "checkpoint.json" && name != "proposal.patch" && !strings.HasPrefix(name, "candidate/") {
			return artifact, nil, publicationPolicy("unexpected checkpoint file %s", name)
		}
	}
	parent, err := os.MkdirTemp("", "verifier-checkpoint-")
	if err != nil {
		return artifact, nil, err
	}
	defer os.RemoveAll(parent)
	dir := filepath.Join(parent, "data")
	if err = writeVerifierFiles(dir, files); err != nil {
		return artifact, nil, err
	}
	if err = VerifyCandidate(filepath.Join(dir, "candidate"), h.CandidateSHA256, h.Target.RefName, h.Target.RefKind, h.Target.PeeledCommitSHA); err != nil {
		return artifact, nil, err
	}
	return artifact, files, nil
}

// VerifierContinuationTitle is a projection of native event identity. Only runs
// from the fixed trusted main workflow are considered by the duplicate check.
func VerifierContinuationTitle(number int, head string) string {
	return fmt.Sprintf("Verifier continuation PR %d H %s", number, head)
}

func priorVerifierContinuation(req VerifierPublicationRequest, pub VerifierPublication, currentID int64) (bool, error) {
	current, err := readVerifierRun(req, currentID)
	if err != nil {
		return false, err
	}
	title := VerifierContinuationTitle(pub.Number, pub.Head)
	if current.Event != "pull_request" || current.Status != "in_progress" || current.HeadSHA != pub.Head || current.DisplayTitle != title {
		return false, publicationPolicy("current continuation is not the trusted merged verifier event")
	}
	bound := false
	for _, pr := range current.PullRequests {
		bound = bound || pr.Number == pub.Number && pr.Head.SHA == pub.Head
	}
	if !bound {
		return false, publicationPolicy("continuation run has no native exact verifier PR binding")
	}
	if current.RunAttempt > 1 {
		return true, nil
	}
	count, total := 0, -1
	for page := 1; page <= 100; page++ {
		var batch struct {
			Total int                 `json:"total_count"`
			Runs  []verifierNativeRun `json:"workflow_runs"`
		}
		query := fmt.Sprintf("repos/%s/actions/workflows/codexsdk-upstream-protocol-sync.yml/runs?event=pull_request&head_sha=%s&per_page=100&page=%d", req.Repository, pub.Head, page)
		if err = req.API.Request("GET", query, nil, &batch); err != nil {
			return false, err
		}
		if total < 0 {
			total = batch.Total
		} else if total != batch.Total {
			return false, publicationPolicy("continuation run listing changed")
		}
		count += len(batch.Runs)
		for _, run := range batch.Runs {
			if run.ID >= currentID {
				// A later queued event must not suppress the first event. The
				// shared workflow concurrency serializes their actual starts.
				continue
			}
			if run.ID <= 0 {
				return false, publicationPolicy("invalid native continuation run")
			}
			if !verifierWorkflowPath(run.Path, verifierWorkflow) || run.Repository.FullName != req.Repository || run.HeadRepository.FullName != req.Repository || run.HeadSHA != pub.Head || run.Event != "pull_request" || run.DisplayTitle != title {
				continue
			}
			for _, pr := range run.PullRequests {
				if pr.Number == pub.Number && pr.Head.SHA == pub.Head {
					return true, nil
				}
			}
		}
		if len(batch.Runs) < 100 {
			if count != total {
				return false, publicationPolicy("incomplete continuation run listing")
			}
			return false, nil
		}
	}
	return false, publicationPolicy("continuation run listing exceeds bound")
}

// ContinueVerifier must run within the existing serialized sync workflow. It
// validates only, and restores immutable data into a fresh directory if asked.
// It never resets consumed budget, applies a proposal, or reuses acceptance.
func ContinueVerifier(request VerifierContinuationRequest) (VerifierContinuation, error) {
	var result VerifierContinuation
	req, err := verifierRequest(request.VerifierPublicationRequest)
	if err != nil {
		return result, err
	}
	req.CurrentRunID = 0 // prior artifact-producing run must now be terminal
	request.VerifierPublicationRequest = req
	request.Merged = true
	pub, err := ResolveVerifier(request.VerifierResolveRequest)
	if err != nil {
		return result, err
	}
	req.OriginRunID, req.OriginAttempt = pub.Handoff.OriginRunID, pub.Handoff.OriginAttempt
	duplicate, err := priorVerifierContinuation(req, pub, request.ContinuationRunID)
	if err != nil {
		return result, err
	}
	result = VerifierContinuation{Publication: pub, MainSHA: pub.Head, RemainingRounds: pub.Handoff.MaxRounds - pub.Handoff.RoundsUsed, Duplicate: duplicate}
	if duplicate {
		return result, nil
	}
	now := time.Now
	if request.Now != nil {
		now = request.Now
	}
	deadline, err := time.Parse(time.RFC3339, pub.Handoff.DeadlineUTC)
	if err != nil || !now().Before(deadline) {
		return result, publicationPolicy("original repair deadline expired; continuation cannot extend it")
	}
	artifact, files, err := loadVerifierCheckpoint(req, pub.Handoff)
	if err != nil {
		return result, err
	}
	result.CheckpointArtifactID, result.CheckpointDigest = artifact.ID, artifact.Digest
	// Reobserve mutable Git facts immediately before exposing recovery bytes.
	actual, err := remotePublicationHead(req.RepoRoot, req.Remote, req.BaseBranch)
	if err != nil {
		return result, err
	}
	if actual != pub.Head {
		return result, publicationPolicy("main changed during verifier continuation")
	}
	if err = freshVerifierTarget(req, pub.Handoff); err != nil {
		return result, err
	}
	if !now().Before(deadline) {
		return result, publicationPolicy("original repair deadline expired during continuation validation")
	}
	if request.OutputDir != "" {
		if err = writeVerifierFiles(request.OutputDir, files); err != nil {
			return result, err
		}
		result.CandidateDir = filepath.Join(request.OutputDir, "candidate")
		result.ProposalPatchPath = filepath.Join(request.OutputDir, "proposal.patch")
	}
	return result, nil
}

// ResolveVerifierCompletedRun filters the existing PR-verification completion
// event without granting a second integration authority. Non-verifier runs are
// not applicable; a verifier-looking run must satisfy full native provenance.
// Auto-forward must still perform its four exact-head checks and review policy.
func ResolveVerifierCompletedRun(request VerifierPublicationRequest, workflowRunID int64) (VerifierPublication, bool, error) {
	var result VerifierPublication
	if workflowRunID <= 0 || !verifierRepositoryRE.MatchString(request.Repository) {
		return result, false, publicationPolicy("verification completion requires repository and native run ID")
	}
	api := request.ReadAPI
	if api == nil {
		api = request.API
	}
	if api == nil {
		api = verifierReadAPI{repoRoot: request.RepoRoot}
	}
	var run verifierNativeRun
	if err := api.Request("GET", fmt.Sprintf("repos/%s/actions/runs/%d", request.Repository, workflowRunID), nil, &run); err != nil {
		return result, false, err
	}
	if !strings.HasPrefix(run.HeadBranch, verifierBranchPrefix) {
		return result, false, nil
	}
	req, err := verifierRequest(request)
	if err != nil {
		return result, true, err
	}
	if run.ID != workflowRunID || !verifierWorkflowPath(run.Path, ".github/workflows/pr-verification.yml") || run.Event != "pull_request" || run.Status != "completed" || run.Conclusion != "success" || run.Repository.FullName != req.Repository || run.HeadRepository.FullName != req.Repository || !shaRE.MatchString(run.HeadSHA) || len(run.PullRequests) != 1 || run.PullRequests[0].Number <= 0 || run.PullRequests[0].Head.SHA != run.HeadSHA {
		return result, true, publicationPolicy("verifier completion is not a successful native exact-head PR verification")
	}
	req.CurrentRunID = 0
	result, err = ResolveVerifier(VerifierResolveRequest{VerifierPublicationRequest: req, Number: run.PullRequests[0].Number, ExpectedHead: run.HeadSHA})
	if err != nil {
		return result, true, err
	}
	if result.Branch != run.HeadBranch {
		return VerifierPublication{}, true, publicationPolicy("verification completion branch differs from verifier publication")
	}
	return result, true, nil
}

// Read and effect credentials remain separate even inside the publisher. The
// publication App needs no Actions permission; only the built-in read token is
// sent to the native run/artifact API. No token is an argument or persisted fact.
type verifierRoutedAPI struct{ read, write PublicationAPI }

func (api verifierRoutedAPI) Request(method, path string, body, result any) error {
	if method == "GET" {
		return api.read.Request(method, path, body, result)
	}
	return api.write.Request(method, path, body, result)
}

type verifierReadAPI struct{ repoRoot string }

func (api verifierReadAPI) Request(method, path string, body, result any) error {
	if method != "GET" || body != nil {
		return publicationPolicy("verifier read authority cannot perform an effect")
	}
	command := exec.Command("gh", "api", "--method", "GET", path)
	command.Dir = api.repoRoot
	command.Env = verifierReadEnvironment()
	var stderr bytes.Buffer
	command.Stderr = &stderr
	raw, err := command.Output()
	if err != nil {
		return fmt.Errorf("GitHub GET %s: %w: %s", path, err, stderr.String())
	}
	if result == nil {
		return nil
	}
	return json.Unmarshal(raw, result)
}
func verifierReadEnvironment() []string {
	token := os.Getenv("GH_READ_TOKEN")
	if token == "" {
		return os.Environ()
	}
	var env []string
	for _, entry := range os.Environ() {
		if !strings.HasPrefix(entry, "GH_TOKEN=") && !strings.HasPrefix(entry, "GH_READ_TOKEN=") && !strings.HasPrefix(entry, "GITHUB_TOKEN=") {
			env = append(env, entry)
		}
	}
	return append(env, "GH_TOKEN="+token)
}

// ValidateVerifierProducer is the read-only job's precondition before minting an
// App token. It loads only native-bound artifacts, checks retained provenance,
// scope and reviewed tree in an isolated index, and never writes remote state.
// The local checkout must be the exact accepted origin base, not proposal code.
func validateVerifierProducer(request VerifierPublicationRequest) (verifierBundle, error) {
	var empty verifierBundle
	req, err := verifierRequest(request)
	if err != nil {
		return empty, err
	}
	if err = AssertClean(req.RepoRoot); err != nil {
		return empty, err
	}
	bundle, err := loadVerifierBundle(req)
	if err != nil {
		return empty, err
	}
	if _, _, err = loadVerifierCheckpoint(req, bundle.handoff); err != nil {
		return empty, err
	}
	head, err := gitOutput(req.RepoRoot, "rev-parse", "HEAD")
	if err != nil {
		return empty, err
	}
	if strings.TrimSpace(head) != bundle.handoff.BaseSHA {
		return empty, publicationPolicy("verifier producer validation requires its accepted-base checkout")
	}
	main, err := remotePublicationHead(req.RepoRoot, req.Remote, req.BaseBranch)
	if err != nil {
		return empty, err
	}
	if main != bundle.handoff.BaseSHA {
		return empty, publicationPolicy("main moved before verifier producer validation")
	}
	if err = freshVerifierTarget(req, bundle.handoff); err != nil {
		return empty, err
	}
	tree, err := verifierPatchTree(req.RepoRoot, bundle.handoff.BaseSHA, bundle.files["verifier.patch"])
	if err != nil {
		return empty, err
	}
	if tree != bundle.handoff.ReviewedTree {
		return empty, publicationPolicy("verifier artifact does not reconstruct the reviewed tree")
	}
	return bundle, nil
}

func ValidateVerifierProducer(request VerifierPublicationRequest) (VerifierHandoff, error) {
	bundle, err := validateVerifierProducer(request)
	return bundle.handoff, err
}

// PrepareVerifierPublication performs the read-only native preflight before
// materializing only the bound verifier patch locally. The caller may commit
// that exact staged tree, then mint the existing publication App token. No
// proposed source is executed and no remote write or credential mint occurs.
func PrepareVerifierPublication(request VerifierPublicationRequest) (VerifierHandoff, error) {
	bundle, err := validateVerifierProducer(request)
	if err != nil {
		return VerifierHandoff{}, err
	}
	command := exec.Command("git", "-C", request.RepoRoot, "apply", "--index", "--whitespace=nowarn", "-")
	command.Stdin = bytes.NewReader(bundle.files["verifier.patch"])
	if raw, e := command.CombinedOutput(); e != nil {
		return VerifierHandoff{}, fmt.Errorf("prepare verifier patch: %w: %s", e, raw)
	}
	if err = CheckScope(request.RepoRoot, "verifier"); err != nil {
		return VerifierHandoff{}, err
	}
	tree, err := gitOutput(request.RepoRoot, "write-tree")
	if err != nil {
		return VerifierHandoff{}, err
	}
	if strings.TrimSpace(tree) != bundle.handoff.ReviewedTree {
		return VerifierHandoff{}, publicationPolicy("prepared verifier tree changed after preflight")
	}
	return bundle.handoff, nil
}
