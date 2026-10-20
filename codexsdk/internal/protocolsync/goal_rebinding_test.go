package protocolsync

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGoalRebindingPolicy(t *testing.T) {
	identity := func(tag, sha string) BaselineIdentity {
		return BaselineIdentity{SourceRefKind: KindStableTag, SourceRefName: tag, SourceCommit: strings.Repeat(sha, 40)}
	}
	original := identity("rust-v0.160.0", "a")
	target := identity("rust-v0.162.0", "c")
	request := GoalRebindingRequest{OriginalBaseSHA: strings.Repeat("1", 40), CurrentBaseSHA: strings.Repeat("2", 40), OriginalBaseline: original, CurrentBaseline: original, Target: target}
	tests := []struct {
		name   string
		change func(*GoalRebindingRequest)
		want   string
	}{
		{"unrelated main", func(r *GoalRebindingRequest) {}, GoalRebindingRun},
		{"intermediate baseline", func(r *GoalRebindingRequest) { r.CurrentBaseline = identity("rust-v0.161.0", "b") }, GoalRebindingRun},
		{"exact accepted", func(r *GoalRebindingRequest) { r.CurrentBaseline = target }, GoalRebindingAlreadyAccepted},
		{"superseded", func(r *GoalRebindingRequest) { r.CurrentBaseline = identity("rust-v0.163.0", "d") }, GoalRebindingSuperseded},
		{"moved target tag", func(r *GoalRebindingRequest) { r.CurrentBaseline = identity(target.SourceRefName, "d") }, GoalRebindingBlocked},
		{"moved original tag", func(r *GoalRebindingRequest) { r.CurrentBaseline = identity(original.SourceRefName, "d") }, GoalRebindingBlocked},
		{"downgraded main", func(r *GoalRebindingRequest) { r.CurrentBaseline = identity("rust-v0.159.0", "d") }, GoalRebindingBlocked},
		{"downgraded target", func(r *GoalRebindingRequest) { r.Target = identity("rust-v0.159.0", "d") }, GoalRebindingBlocked},
		{"missing baseline", func(r *GoalRebindingRequest) { r.CurrentBaseline = BaselineIdentity{} }, GoalRebindingBlocked},
		{"manual main", func(r *GoalRebindingRequest) { r.CurrentBaseline.SourceRefKind = KindManualRef }, GoalRebindingBlocked},
		{"manual target", func(r *GoalRebindingRequest) { r.Target.SourceRefKind = KindManualCommit }, GoalRebindingBlocked},
		{"ambiguous commit", func(r *GoalRebindingRequest) { r.Target.SourceCommit = original.SourceCommit }, GoalRebindingBlocked},
		{"non exact main", func(r *GoalRebindingRequest) { r.CurrentBaseSHA = "main" }, GoalRebindingBlocked},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := request
			tt.change(&r)
			if got := EvaluateGoalRebinding(r); got.Decision != tt.want {
				t.Fatalf("got %+v want %s", got, tt.want)
			}
		})
	}
}

func TestResolveGoalRebindingUsesExactCommitMetadata(t *testing.T) {
	root := initSyncRepo(t, strings.Repeat("a", 40), "rust-v0.160.0", KindStableTag)
	original, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "unrelated.txt"), "main advances")
	if err := runGit(root, "add", "unrelated.txt"); err != nil {
		t.Fatal(err)
	}
	if err := runGit(root, "commit", "-m", "unrelated main"); err != nil {
		t.Fatal(err)
	}
	current, err := gitOutput(root, "rev-parse", "HEAD")
	if err != nil {
		t.Fatal(err)
	}
	target := BaselineIdentity{SourceRefKind: KindStableTag, SourceRefName: "rust-v0.162.0", SourceCommit: strings.Repeat("c", 40)}
	result, err := ResolveGoalRebinding(root, strings.TrimSpace(original), strings.TrimSpace(current), target)
	if err != nil || result.Decision != GoalRebindingRun {
		t.Fatalf("%+v %v", result, err)
	}
	if _, err := ResolveGoalRebinding(root, strings.Repeat("0", 40), strings.TrimSpace(current), target); err == nil {
		t.Fatal("missing original accepted")
	}
}

func TestGoalRebindingChecksBothAcceptedBaselines(t *testing.T) {
	for _, broken := range []string{"", "original", "current"} {
		t.Run(broken, func(t *testing.T) {
			req := goalContractFixtureRequest(t, goalFixture(t), "provenance")
			req.OriginalBaseRoot = goalFixture(t)
			for label, root := range map[string]string{"original": req.OriginalBaseRoot, "current": req.BaseRoot} {
				writeFile(t, filepath.Join(root, "baseline-label"), label)
				if err := runGit(root, "add", "baseline-label"); err != nil {
					t.Fatal(err)
				}
				if err := runGit(root, "commit", "-m", label); err != nil {
					t.Fatal(err)
				}
			}
			seen := map[string]bool{}
			result, err := verifyGoal(context.Background(), req, goalTestSuccess, func(_ context.Context, r GoalContractRequest) error {
				data, e := os.ReadFile(filepath.Join(r.BaseRoot, "baseline-label"))
				if e != nil {
					return e
				}
				label := string(data)
				seen[label] = true
				if label == broken {
					return errors.New("accepted public regression")
				}
				return nil
			})
			defer result.Close()
			if (err != nil) != (broken != "") {
				t.Fatalf("broken %q: %v", broken, err)
			}
			if !seen["original"] || !seen["current"] {
				t.Fatalf("lost baseline obligation: %v", seen)
			}
		})
	}
}

func TestGoalRebindingMissingOriginalContractBlocks(t *testing.T) {
	req := goalContractFixtureRequest(t, goalFixture(t), "provenance")
	req.OriginalBaseRoot = filepath.Join(t.TempDir(), "missing")
	result, err := verifyGoal(context.Background(), req, goalTestSuccess, goalTestContract)
	defer result.Close()
	if err == nil || len(result.Gates) != 0 {
		t.Fatal("missing original contract accepted", result, err)
	}
}
