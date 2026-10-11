package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"github.com/ronhuafeng/llm-go/codexsdk/internal/protocolsync"
	"io"
	"os"
	"strconv"
	"strings"
)

func verifierOutput(values map[string]string) error {
	path := os.Getenv("GITHUB_OUTPUT")
	if path == "" {
		return nil
	}
	f, e := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0600)
	if e != nil {
		return e
	}
	defer f.Close()
	for key, value := range values {
		if strings.ContainsAny(key+value, "\r\n") {
			return fmt.Errorf("invalid verifier output")
		}
		if _, e := fmt.Fprintf(f, "%s=%s\n", key, value); e != nil {
			return e
		}
	}
	return nil
}
func verifierRequestFlags(fs *flag.FlagSet) (*protocolsync.VerifierPublicationRequest, *int, *string) {
	req := &protocolsync.VerifierPublicationRequest{}
	fs.StringVar(&req.RepoRoot, "repo-root", "", "trusted accepted repository")
	fs.StringVar(&req.Repository, "github-repository", "", "native repository")
	fs.StringVar(&req.BaseBranch, "base-branch", "main", "accepted default branch")
	fs.StringVar(&req.Remote, "remote", "origin", "repository remote")
	fs.Int64Var(&req.AppBotID, "app-bot-id", 0, "native publishing App identity")
	number := fs.Int("pr-number", 0, "verifier proposal number")
	head := fs.String("head", "", "exact proposal head")
	return req, number, head
}
func runVerifierPublication(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("publish-verifier", flag.ContinueOnError)
	fs.SetOutput(stderr)
	req, _, _ := verifierRequestFlags(fs)
	fs.Int64Var(&req.OriginRunID, "origin-run-id", 0, "trusted producer run")
	fs.IntVar(&req.OriginAttempt, "origin-attempt", 0, "trusted producer attempt")
	fs.Int64Var(&req.CurrentRunID, "current-run-id", 0, "current publishing run")
	if e := fs.Parse(args); e != nil {
		return 2
	}
	publication, e := protocolsync.PublishVerifier(*req)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	if e := verifierOutput(map[string]string{"pr_number": strconv.Itoa(publication.Number), "head": publication.Head, "pr_url": publication.URL}); e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	_ = json.NewEncoder(stdout).Encode(publication)
	return 0
}
func runVerifierResolution(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("resolve-verifier", flag.ContinueOnError)
	fs.SetOutput(stderr)
	req, number, head := verifierRequestFlags(fs)
	completedRun := fs.Int64("verification-run-id", 0, "native PR verification completion")
	if e := fs.Parse(args); e != nil {
		return 2
	}
	var p protocolsync.VerifierPublication
	var e error
	found := true
	if *completedRun > 0 {
		p, found, e = protocolsync.ResolveVerifierCompletedRun(*req, *completedRun)
	} else {
		p, e = protocolsync.ResolveVerifier(protocolsync.VerifierResolveRequest{VerifierPublicationRequest: *req, Number: *number, ExpectedHead: *head})
	}
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	if !found {
		return 0
	}
	if e := verifierOutput(map[string]string{"pr_number": strconv.Itoa(p.Number), "head": p.Head}); e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	_ = json.NewEncoder(stdout).Encode(p)
	return 0
}
func runVerifierContinuation(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("continue-verifier", flag.ContinueOnError)
	fs.SetOutput(stderr)
	req, number, head := verifierRequestFlags(fs)
	current := fs.Int64("current-run-id", 0, "current merged-event run")
	output := fs.String("output-dir", "", "new private directory for verified checkpoint")
	if e := fs.Parse(args); e != nil {
		return 2
	}
	c, result, e := protocolsync.PrepareVerifierContinuation(protocolsync.VerifierContinuationRequest{VerifierResolveRequest: protocolsync.VerifierResolveRequest{VerifierPublicationRequest: *req, Number: *number, ExpectedHead: *head, Merged: true}, ContinuationRunID: *current, OutputDir: *output})
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	values := map[string]string{"duplicate": strconv.FormatBool(c.Duplicate), "remaining_rounds": strconv.Itoa(c.RemainingRounds), "main_sha": c.MainSHA, "target_ref": c.Publication.Handoff.Target.RefName, "target_kind": c.Publication.Handoff.Target.RefKind, "target_sha": c.Publication.Handoff.Target.PeeledCommitSHA, "candidate_sha256": c.Publication.Handoff.CandidateSHA256}
	if e := verifierOutput(values); e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	if e := protocolsync.WriteGitHubOutput(os.Getenv("GITHUB_OUTPUT"), result); e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	_ = json.NewEncoder(stdout).Encode(c)
	return 0
}

func runVerifierPreparation(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("prepare-verifier", flag.ContinueOnError)
	fs.SetOutput(stderr)
	req, _, _ := verifierRequestFlags(fs)
	fs.Int64Var(&req.OriginRunID, "origin-run-id", 0, "trusted producer run")
	fs.IntVar(&req.OriginAttempt, "origin-attempt", 0, "trusted producer attempt")
	fs.Int64Var(&req.CurrentRunID, "current-run-id", 0, "current publishing run")
	if e := fs.Parse(args); e != nil {
		return 2
	}
	h, e := protocolsync.PrepareVerifierPublication(*req)
	if e != nil {
		fmt.Fprintln(stderr, e)
		return 1
	}
	_ = json.NewEncoder(stdout).Encode(h)
	return 0
}
