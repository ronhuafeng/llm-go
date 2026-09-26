package protocolupgrade

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// CheckCompatibility compiles the public SDK packages and their tests against
// the prepared bytes without executing any package or test initializer. The Go
// overlay leaves the accepted source untouched and is discarded after checking.
// A failing accepted build is not evidence of a candidate incompatibility.
func (p PlanResult) CheckCompatibility() (PlanResult, error) {
	if p.Status != PlanReady {
		return p, nil
	}
	if p.prepared == nil {
		return PlanResult{}, fmt.Errorf("compatibility check requires a prepared candidate")
	}
	module, err := filepath.Abs(p.prepared.request.ModuleRoot)
	if err != nil {
		return PlanResult{}, err
	}
	tmp, err := os.MkdirTemp("", "protocolupgrade-compile-")
	if err != nil {
		return PlanResult{}, err
	}
	defer os.RemoveAll(tmp)
	replacements := map[string]string{}
	for name, data := range p.prepared.files {
		if !strings.HasSuffix(name, ".go") {
			continue
		}
		target := filepath.Join(tmp, "candidate", name)
		if err := os.MkdirAll(filepath.Dir(target), 0755); err != nil {
			return PlanResult{}, err
		}
		if err := os.WriteFile(target, data, 0644); err != nil {
			return PlanResult{}, err
		}
		replacements[filepath.Join(module, name)] = target
	}
	overlay, err := json.Marshal(struct{ Replace map[string]string }{replacements})
	if err != nil {
		return PlanResult{}, err
	}
	overlayPath := filepath.Join(tmp, "overlay.json")
	if err := os.WriteFile(overlayPath, overlay, 0644); err != nil {
		return PlanResult{}, err
	}
	build := func(candidate bool) ([]byte, error) {
		args := []string{"test", "-c", "-vet=off", "-mod=readonly", "-o", tmp + string(filepath.Separator)}
		if candidate {
			args = append(args, "-overlay", overlayPath)
		}
		args = append(args, "./codexsdk", "./codexsdk/protocolv2")
		cmd := exec.Command("go", args...)
		cmd.Dir = filepath.Dir(module)
		for _, entry := range os.Environ() {
			key, _, _ := strings.Cut(entry, "=")
			if key != "GOWORK" && key != "GOFLAGS" && key != "GOTOOLCHAIN" {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "GOWORK=off", "GOFLAGS=", "GOTOOLCHAIN=local")
		return cmd.CombinedOutput()
	}
	output, err := build(true)
	if err == nil {
		return p, nil
	}
	accepted, acceptedErr := build(false)
	if acceptedErr != nil {
		return PlanResult{}, fmt.Errorf("cannot establish candidate compatibility: candidate build: %v\n%s\naccepted build: %v\n%s", err, output, acceptedErr, accepted)
	}
	return PlanResult{Status: PlanSemanticUnresolved, Issue: &PlanIssue{Stage: "go_compatibility", Path: "codexsdk", Reason: fmt.Sprintf("candidate SDK/test compilation failed: %v\n%s", err, output)}}, nil
}

// PlanRepository includes compilation of handwritten SDK source and tests in
// the read-only plan used by synchronization and the command-line owner.
func PlanRepository(req ApplyRequest) (PlanResult, error) {
	planned, err := Plan(req)
	if err != nil {
		return planned, err
	}
	return planned.CheckCompatibility()
}
