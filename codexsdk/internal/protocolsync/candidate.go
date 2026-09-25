package protocolsync

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// candidateDigest covers the complete per-run candidate, including both schema
// sets, reports, and the exact common.rs mapping. Symlinks and special files
// are rejected so an ignored cache path cannot redirect accepted inputs.
func candidateDigest(dir string) (string, error) {
	root, err := os.Lstat(dir)
	if err != nil {
		return "", fmt.Errorf("candidate root: %w", err)
	}
	if !root.IsDir() {
		return "", fmt.Errorf("candidate root %s is not a directory", dir)
	}
	for _, required := range []string{"schema", "stable-schema", "reports", "common.rs", "common.rs.source_sha"} {
		info, err := os.Lstat(filepath.Join(dir, required))
		if err != nil {
			return "", fmt.Errorf("candidate %s: %w", required, err)
		}
		if strings.HasSuffix(required, ".rs") || strings.HasSuffix(required, ".source_sha") {
			if !info.Mode().IsRegular() {
				return "", fmt.Errorf("candidate %s is not a regular file", required)
			}
		} else if !info.IsDir() {
			return "", fmt.Errorf("candidate %s is not a directory", required)
		}
	}
	var entries []string
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == dir {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			entries = append(entries, "d\x00"+rel)
			return nil
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("candidate %s is not a regular file", rel)
		}
		file, err := os.Open(path)
		if err != nil {
			return err
		}
		fileHash := sha256.New()
		_, copyErr := io.Copy(fileHash, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		entries = append(entries, "f\x00"+rel+"\x00"+hex.EncodeToString(fileHash.Sum(nil)))
		return nil
	})
	if err != nil {
		return "", err
	}
	sort.Strings(entries)
	combined := sha256.New()
	for _, entry := range entries {
		_, _ = combined.Write(binary.BigEndian.AppendUint64(nil, uint64(len(entry))))
		_, _ = combined.Write([]byte(entry))
	}
	return hex.EncodeToString(combined.Sum(nil)), nil
}

func copyVerifiedCandidate(dir, expected, targetRef, targetKind, targetSHA string) (string, func(), error) {
	if len(expected) != sha256.Size*2 {
		return "", nil, fmt.Errorf("expected candidate sha256 is required")
	}
	if _, err := hex.DecodeString(expected); err != nil {
		return "", nil, fmt.Errorf("invalid expected candidate sha256: %w", err)
	}
	initial, err := candidateDigest(dir)
	if err != nil {
		return "", nil, err
	}
	if initial != strings.ToLower(expected) {
		return "", nil, fmt.Errorf("candidate changed after initial Plan: sha256 %s, expected %s", initial, expected)
	}
	tmp, err := os.MkdirTemp("", "codexsdk-resume-candidate-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tmp) }
	err = filepath.WalkDir(dir, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		dst := filepath.Join(tmp, rel)
		if entry.IsDir() {
			return os.MkdirAll(dst, 0o755)
		}
		if !entry.Type().IsRegular() {
			return fmt.Errorf("candidate %s is not a regular file", filepath.ToSlash(rel))
		}
		in, err := os.Open(path)
		if err != nil {
			return err
		}
		out, err := os.OpenFile(dst, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o644)
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(out, in)
		closeErr := out.Close()
		inCloseErr := in.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		return inCloseErr
	})
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("copy candidate: %w", err)
	}
	actual, err := candidateDigest(tmp)
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if actual != strings.ToLower(expected) {
		cleanup()
		return "", nil, fmt.Errorf("candidate changed after initial Plan: sha256 %s, expected %s", actual, expected)
	}
	marker, err := os.ReadFile(filepath.Join(tmp, "common.rs.source_sha"))
	if err != nil {
		cleanup()
		return "", nil, err
	}
	if strings.TrimSpace(string(marker)) != targetSHA {
		cleanup()
		return "", nil, fmt.Errorf("candidate common.rs source %q does not match target %s", strings.TrimSpace(string(marker)), targetSHA)
	}
	reportBytes, err := os.ReadFile(filepath.Join(tmp, "reports", "drift_summary.json"))
	if err != nil {
		cleanup()
		return "", nil, fmt.Errorf("candidate drift summary: %w", err)
	}
	var report struct {
		Target struct {
			RefName string `json:"source_ref_name"`
			RefKind string `json:"source_ref_kind"`
			Commit  string `json:"source_commit"`
		} `json:"target"`
	}
	if err := json.Unmarshal(reportBytes, &report); err != nil {
		cleanup()
		return "", nil, fmt.Errorf("candidate drift summary: %w", err)
	}
	if report.Target.RefName != targetRef || report.Target.RefKind != targetKind || report.Target.Commit != targetSHA {
		cleanup()
		return "", nil, fmt.Errorf("candidate report target %s/%s/%s does not match selected %s/%s/%s", report.Target.RefName, report.Target.RefKind, report.Target.Commit, targetRef, targetKind, targetSHA)
	}
	return tmp, cleanup, nil
}

// VerifyCandidate rechecks the immutable candidate after executable proposal
// tests, before their result may be handed to a separate publication runner.
func VerifyCandidate(dir, expected, targetRef, targetKind, targetSHA string) error {
	_, cleanup, err := copyVerifiedCandidate(dir, expected, targetRef, targetKind, targetSHA)
	if err != nil {
		return err
	}
	cleanup()
	return nil
}
