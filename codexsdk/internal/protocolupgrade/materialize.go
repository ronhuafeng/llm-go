package protocolupgrade

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
)

// A prepared candidate is private to a successful Plan. Its bytes belong to
// that construction; materialization never reopens moving upstream inputs.
type preparedCandidate struct {
	request  ApplyRequest
	baseline map[string]string
	files    map[string][]byte
	reports  map[string][]byte
}

func prepareCandidateWrites(req ApplyRequest, root, reports string, baseline map[string]string) (*preparedCandidate, error) {
	files, err := readCandidateFiles(filepath.Join(root, filepath.FromSlash(defaultBaselineRel)))
	if err != nil {
		return nil, err
	}
	owned := make(map[string][]byte, len(files)+len(generatedProtocolArtifacts))
	for name, data := range files {
		owned[filepath.Join(defaultBaselineRel, name)] = data
	}
	for _, name := range generatedProtocolArtifacts {
		data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(name)))
		if err != nil {
			return nil, err
		}
		owned[name] = data
	}
	reportFiles, err := readCandidateFiles(reports)
	if err != nil {
		return nil, err
	}
	return &preparedCandidate{request: req, baseline: baseline, files: owned, reports: reportFiles}, nil
}

func readCandidateFiles(root string) (map[string][]byte, error) {
	files := map[string][]byte{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		files[filepath.ToSlash(rel)] = data
		return nil
	})
	return files, err
}

// Apply writes exactly the successfully constructed candidate. A zero or
// unresolved plan has no write authority; changed baselines require a new Plan.
func (p PlanResult) Apply() (ApplyResult, error) {
	if p.Status != PlanReady || p.prepared == nil {
		return ApplyResult{}, fmt.Errorf("apply requires a successfully constructed candidate")
	}
	prepared := p.prepared
	current, err := snapshotHashes(prepared.request.Baseline)
	if err != nil {
		return ApplyResult{}, err
	}
	if err := sameSnapshot(prepared.baseline, current, "accepted baseline since Plan"); err != nil {
		return ApplyResult{}, err
	}
	root := prepared.request.ModuleRoot
	if root == "" {
		root = filepath.Clean(filepath.Join(prepared.request.Baseline, "../../../.."))
	}
	old, err := schemaFiles(prepared.request.Baseline)
	if err != nil {
		return ApplyResult{}, err
	}
	for _, name := range old {
		if _, exists := prepared.files[filepath.Join(defaultBaselineRel, name)]; !exists {
			if err := os.Remove(filepath.Join(prepared.request.Baseline, name)); err != nil {
				return ApplyResult{}, err
			}
		}
	}
	if err := writeCandidateFiles(root, prepared.files); err != nil {
		return ApplyResult{}, err
	}
	reports := prepared.request.Reports
	if reports == "" {
		reports = filepath.Join(filepath.Dir(prepared.request.Candidate), "reports")
	}
	if err := writeCandidateFiles(reports, prepared.reports); err != nil {
		return ApplyResult{}, err
	}
	return p.Preview, nil
}

func writeCandidateFiles(root string, files map[string][]byte) error {
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		path := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, files[name], 0o644); err != nil {
			return err
		}
	}
	return nil
}
