package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
)

const releaseAuthorizationFormatVersion = 1

type ReleaseAuthorization struct {
	FormatVersion       int                            `json:"format_version"`
	Subject             ReleaseAuthorizationSubject    `json:"subject"`
	Plan                ReleaseAuthorizationArtifact   `json:"plan"`
	Evidence            []ReleaseAuthorizationEvidence `json:"evidence"`
	AuthorizationDigest string                         `json:"authorization_digest"`
}

type ReleaseAuthorizationSubject struct {
	Commit   string `json:"commit"`
	ModuleID string `json:"module_id"`
	Tag      string `json:"tag"`
}

type ReleaseAuthorizationArtifact struct {
	File       string `json:"file"`
	SHA256     string `json:"sha256"`
	PlanDigest string `json:"plan_digest"`
}

type ReleaseAuthorizationEvidence struct {
	Name   string `json:"name"`
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
}

type releaseEvidenceInput struct {
	name  string
	path  string
	kind  string
	stage string
}

func BuildReleaseAuthorization(plan ReleasePlan, planPath, minimumPath, currentPath, racePath, checkoutPath string) (ReleaseAuthorization, error) {
	if err := plan.Validate(); err != nil {
		return ReleaseAuthorization{}, err
	}
	planHash, err := fileSHA256(planPath)
	if err != nil {
		return ReleaseAuthorization{}, fmt.Errorf("hash release plan: %w", err)
	}
	inputs := []releaseEvidenceInput{
		{name: "checkout", path: checkoutPath, kind: "checkout_source"},
		{name: "current", path: currentPath, kind: "module_source", stage: "current"},
		{name: "minimum", path: minimumPath, kind: "module_source", stage: "minimum"},
		{name: "race", path: racePath, kind: "module_source", stage: "race"},
	}
	evidence := make([]ReleaseAuthorizationEvidence, 0, len(inputs))
	for _, input := range inputs {
		if err := validatePreflightEvidence(input.path, plan, input.kind, input.stage); err != nil {
			return ReleaseAuthorization{}, fmt.Errorf("validate %s evidence: %w", input.name, err)
		}
		digest, err := fileSHA256(input.path)
		if err != nil {
			return ReleaseAuthorization{}, err
		}
		evidence = append(evidence, ReleaseAuthorizationEvidence{Name: input.name, File: filepath.Base(input.path), SHA256: digest})
	}
	sort.Slice(evidence, func(i, j int) bool { return evidence[i].Name < evidence[j].Name })
	authorization := ReleaseAuthorization{
		FormatVersion: releaseAuthorizationFormatVersion,
		Subject:       ReleaseAuthorizationSubject{Commit: plan.Subject.Commit, ModuleID: plan.Subject.ModuleID, Tag: plan.Subject.Tag},
		Plan:          ReleaseAuthorizationArtifact{File: filepath.Base(planPath), SHA256: planHash, PlanDigest: plan.PlanDigest},
		Evidence:      evidence,
	}
	authorization.AuthorizationDigest, err = releaseAuthorizationDigest(authorization)
	if err != nil {
		return ReleaseAuthorization{}, err
	}
	if err := authorization.Validate(); err != nil {
		return ReleaseAuthorization{}, err
	}
	return authorization, nil
}

func validatePreflightEvidence(path string, plan ReleasePlan, kind, stage string) error {
	var evidence Evidence
	if err := readStrictJSON(path, &evidence); err != nil {
		return err
	}
	if evidence.FormatVersion != evidenceFormatVersion || evidence.Subject.Commit != plan.Subject.Commit || evidence.Subject.Tree != plan.Subject.Tree || evidence.Subject.Kind != kind {
		return fmt.Errorf("evidence subject does not match release commit and kind")
	}
	if kind == "module_source" && (evidence.Subject.Module != plan.Subject.ModuleID || evidence.Subject.Stage != stage) {
		return fmt.Errorf("module evidence does not match %s/%s", plan.Subject.ModuleID, stage)
	}
	if len(evidence.Checks) == 0 {
		return fmt.Errorf("evidence contains no checks")
	}
	for _, check := range evidence.Checks {
		if check.Status != "passed" || check.Error != "" {
			return fmt.Errorf("check %q is not passed", check.Name)
		}
	}
	return nil
}

func releaseAuthorizationDigest(authorization ReleaseAuthorization) (string, error) {
	authorization.AuthorizationDigest = ""
	data, err := json.Marshal(authorization)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(data)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func (authorization ReleaseAuthorization) Validate() error {
	if authorization.FormatVersion != releaseAuthorizationFormatVersion || authorization.Subject.Commit == "" || authorization.Subject.ModuleID == "" || authorization.Subject.Tag == "" {
		return fmt.Errorf("release authorization subject is incomplete")
	}
	if authorization.Plan.File != "release-plan.json" || !isSHA256(authorization.Plan.SHA256) || authorization.Plan.PlanDigest == "" {
		return fmt.Errorf("release authorization plan is invalid")
	}
	if len(authorization.Evidence) != 4 || !sort.SliceIsSorted(authorization.Evidence, func(i, j int) bool { return authorization.Evidence[i].Name < authorization.Evidence[j].Name }) {
		return fmt.Errorf("release authorization evidence set is incomplete or unsorted")
	}
	wantNames := []string{"checkout", "current", "minimum", "race"}
	for index, artifact := range authorization.Evidence {
		if artifact.Name != wantNames[index] || filepath.Base(artifact.File) != artifact.File || !isSHA256(artifact.SHA256) {
			return fmt.Errorf("release authorization evidence artifact is invalid")
		}
	}
	want, err := releaseAuthorizationDigest(authorization)
	if err != nil {
		return err
	}
	if authorization.AuthorizationDigest == "" || authorization.AuthorizationDigest != want {
		return fmt.Errorf("release authorization digest mismatch")
	}
	return nil
}

func ReadReleaseAuthorization(path string) (ReleaseAuthorization, error) {
	var authorization ReleaseAuthorization
	if err := readStrictJSON(path, &authorization); err != nil {
		return ReleaseAuthorization{}, fmt.Errorf("read release authorization: %w", err)
	}
	if err := authorization.Validate(); err != nil {
		return ReleaseAuthorization{}, err
	}
	return authorization, nil
}

func WriteReleaseAuthorization(path string, authorization ReleaseAuthorization) error {
	if err := authorization.Validate(); err != nil {
		return err
	}
	return writeJSON(path, authorization)
}

func ValidateReleaseAuthorizationFiles(plan ReleasePlan, authorization ReleaseAuthorization, directory string) error {
	if err := authorization.Validate(); err != nil {
		return err
	}
	if err := plan.Validate(); err != nil {
		return err
	}
	if authorization.Subject.Commit != plan.Subject.Commit || authorization.Subject.ModuleID != plan.Subject.ModuleID || authorization.Subject.Tag != plan.Subject.Tag || authorization.Plan.PlanDigest != plan.PlanDigest {
		return fmt.Errorf("release authorization does not match release plan")
	}
	planPath := filepath.Join(directory, authorization.Plan.File)
	if digest, err := fileSHA256(planPath); err != nil || digest != authorization.Plan.SHA256 {
		return fmt.Errorf("release plan artifact hash mismatch")
	}
	expected := map[string]releaseEvidenceInput{
		"checkout": {kind: "checkout_source"},
		"current":  {kind: "module_source", stage: "current"},
		"minimum":  {kind: "module_source", stage: "minimum"},
		"race":     {kind: "module_source", stage: "race"},
	}
	for _, artifact := range authorization.Evidence {
		path := filepath.Join(directory, artifact.File)
		digest, err := fileSHA256(path)
		if err != nil || digest != artifact.SHA256 {
			return fmt.Errorf("%s evidence artifact hash mismatch", artifact.Name)
		}
		input := expected[artifact.Name]
		if err := validatePreflightEvidence(path, plan, input.kind, input.stage); err != nil {
			return fmt.Errorf("validate %s evidence: %w", artifact.Name, err)
		}
	}
	return nil
}
