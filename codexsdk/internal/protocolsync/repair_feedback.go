package protocolsync

import (
	"bufio"
	"bytes"
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
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// Disk-only diagnostic limit, independent of the existing console and model
// budgets. A partial artifact is never handed to the repair author as complete.
const repairFeedbackLimit = 16 << 20
const feedbackFooterReserve = 1024

type feedbackRef struct {
	File   string `json:"file"`
	SHA256 string `json:"sha256"`
	Round  int    `json:"round"`
}
type feedbackIdentity struct {
	Base      string `json:"base"`
	Target    Target `json:"target"`
	Candidate string `json:"candidate_sha256"`
	Proposal  string `json:"proposal_sha256"`
	Round     int    `json:"round"`
}
type feedbackRecord struct {
	Kind     string            `json:"kind"`
	Identity *feedbackIdentity `json:"identity,omitempty"`
	Stage    string            `json:"stage,omitempty"`
	Package  string            `json:"package,omitempty"`
	Test     string            `json:"test,omitempty"`
	Trust    string            `json:"trust,omitempty"`
	Output   string            `json:"output,omitempty"`
	Probe    any               `json:"probe,omitempty"`
	Complete *bool             `json:"complete,omitempty"`
	Records  int               `json:"records,omitempty"`
	Redacted bool              `json:"redacted,omitempty"`
}
type feedbackArtifact struct {
	file           *os.File
	dir            string
	identity       feedbackIdentity
	bytes, records int
	err            error
	incomplete     bool
}
type feedbackContextKey struct{}
type feedbackArtifactError struct{ error }

func (e *feedbackArtifactError) Unwrap() error { return e.error }

func newFeedbackArtifact(ctx context.Context, checkpoint string, cp repairCheckpoint, proposal []byte) (context.Context, *feedbackArtifact, error) {
	dir := filepath.Join(checkpoint, "feedback")
	if err := os.MkdirAll(dir, 0700); err != nil {
		return ctx, nil, err
	}
	real, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return ctx, nil, err
	}
	parent, err := filepath.EvalSymlinks(checkpoint)
	if err != nil {
		return ctx, nil, err
	}
	if real != filepath.Join(parent, "feedback") {
		return ctx, nil, errors.New("feedback directory is not controller-owned")
	}
	f, err := os.CreateTemp(dir, "round-*.tmp")
	if err != nil {
		return ctx, nil, err
	}
	a := &feedbackArtifact{file: f, dir: dir, identity: feedbackIdentity{cp.Base, cp.Target, cp.CandidateSHA256, repairDigest(proposal), cp.Round}}
	return context.WithValue(ctx, feedbackContextKey{}, a), a, nil
}
func (a *feedbackArtifact) append(b []byte) {
	if a == nil || a.err != nil || a.incomplete {
		return
	}
	if a.bytes+len(b) > repairFeedbackLimit-feedbackFooterReserve {
		a.incomplete = true
		return
	}
	n, err := a.file.Write(b)
	a.bytes += n
	if err != nil {
		a.err = err
	} else if n != len(b) {
		a.err = io.ErrShortWrite
	}
}
func (a *feedbackArtifact) record(r feedbackRecord) {
	if a == nil {
		return
	}
	b, err := marshalFeedbackRecord(r)
	if err != nil {
		a.err = err
		return
	}
	a.append(append(b, '\n'))
	a.records++
}
func (a *feedbackArtifact) seal(cause error) (feedbackRef, error) {
	if cause != nil {
		a.record(feedbackRecord{Kind: "controller_failure", Output: cause.Error(), Trust: "controller_summary"})
	}
	complete := a.err == nil && !a.incomplete
	header, _ := marshalFeedbackRecord(feedbackRecord{Kind: "header", Identity: &a.identity})
	footer, _ := marshalFeedbackRecord(feedbackRecord{Kind: "footer", Identity: &a.identity, Complete: &complete, Records: a.records + 1})
	if a.bytes+len(header)+len(footer)+2 > repairFeedbackLimit {
		complete = false
		a.incomplete = true
	}
	if !complete {
		footer, _ = marshalFeedbackRecord(feedbackRecord{Kind: "footer", Complete: &complete, Output: "diagnostic capture incomplete or 16 MiB file limit exceeded"})
	}
	if err := a.file.Close(); a.err == nil {
		a.err = err
	}
	if a.err != nil {
		return feedbackRef{}, &feedbackArtifactError{a.err}
	}
	sealedFile, err := os.CreateTemp(a.dir, "sealed-*.tmp")
	if err != nil {
		return feedbackRef{}, &feedbackArtifactError{err}
	}
	if _, err = sealedFile.Write(append(header, '\n')); err == nil {
		var input *os.File
		input, err = os.Open(a.file.Name())
		if err == nil {
			_, err = io.Copy(sealedFile, io.LimitReader(input, int64(repairFeedbackLimit-len(header)-len(footer)-2)))
			if closeErr := input.Close(); err == nil {
				err = closeErr
			}
		}
	}
	if err == nil {
		_, err = sealedFile.Write(append(footer, '\n'))
	}
	if closeErr := sealedFile.Close(); err == nil {
		err = closeErr
	}
	if removeErr := os.Remove(a.file.Name()); err == nil {
		err = removeErr
	}
	a.file = sealedFile
	if err != nil {
		return feedbackRef{}, &feedbackArtifactError{fmt.Errorf("verification diagnostic artifact incomplete: %w", err)}
	}
	if !complete {
		return feedbackRef{}, &feedbackArtifactError{errors.New("verification diagnostic artifact incomplete: size/framing bounds prevented complete capture")}
	}
	sealed := strings.TrimSuffix(sealedFile.Name(), ".tmp") + ".jsonl"
	if err := os.Rename(sealedFile.Name(), sealed); err != nil {
		return feedbackRef{}, &feedbackArtifactError{err}
	}
	digest, _, _, _, err := readFeedbackFile(sealed)
	if err != nil {
		return feedbackRef{}, &feedbackArtifactError{err}
	}
	return feedbackRef{File: filepath.Join("feedback", filepath.Base(sealed)), SHA256: digest, Round: a.identity.Round}, nil
}
func feedbackPath(checkpoint string, ref feedbackRef, cp repairCheckpoint, proposal []byte) (string, error) {
	if filepath.Dir(ref.File) != "feedback" || filepath.Base(ref.File) == "." || filepath.IsAbs(ref.File) {
		return "", errors.New("invalid feedback relative path")
	}
	root, err := filepath.EvalSymlinks(checkpoint)
	if err != nil {
		return "", err
	}
	path := filepath.Join(root, ref.File)
	real, err := filepath.EvalSymlinks(path)
	if err != nil || real != path {
		return "", errors.New("feedback path escaped checkpoint")
	}
	digest, header, footer, count, err := readFeedbackFile(path)
	if err != nil {
		return "", err
	}
	if digest != ref.SHA256 {
		return "", errors.New("feedback digest changed")
	}
	expected := feedbackIdentity{cp.Base, cp.Target, cp.CandidateSHA256, repairDigest(proposal), ref.Round}
	if ref.Round > cp.Round {
		return "", errors.New("feedback round is from the future")
	}
	if footer.Records != count-1 {
		return "", errors.New("feedback record count mismatch")
	}
	if header.Kind != "header" || footer.Kind != "footer" || header.Identity == nil || footer.Identity == nil || *header.Identity != expected || *footer.Identity != expected || footer.Complete == nil || !*footer.Complete {
		return "", errors.New("feedback identity or completeness mismatch")
	}
	return path, nil
}

// Each active test's provisional output is spooled outside both writable
// workspaces. Passing logs are removed; failed logs are copied without the
// 4 KiB console truncation. Overflow of a passing log does not block repair.
type feedbackCapture struct {
	mu       sync.Mutex
	artifact *feedbackArtifact
	stage    string
	pending  map[string]*feedbackPending
	bytes    int
	probe    func(string) any
	secrets  []string
	keep     int
}
type feedbackPending struct {
	path      string
	bytes     int
	truncated bool
	pkg, test string
	tail      string
}

func newFeedbackCapture(ctx context.Context, stage string) *feedbackCapture {
	a, _ := ctx.Value(feedbackContextKey{}).(*feedbackArtifact)
	if a == nil {
		return nil
	}
	c := &feedbackCapture{artifact: a, stage: stage, pending: map[string]*feedbackPending{}, keep: utf8.UTFMax - 1}
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if len(value) >= 8 && (strings.Contains(name, "KEY") || strings.Contains(name, "TOKEN") || strings.Contains(name, "SECRET") || name == "CODEX_RESPONSES_API_ENDPOINT") {
			c.secrets = append(c.secrets, value)
			c.keep = max(c.keep, len(value)-1)
		}
	}
	if c.keep > repairFeedbackLimit {
		a.incomplete = true
		c.keep = 0
	}
	return c
}
func (c *feedbackCapture) event(action, pkg, test, output string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if action == "build-output" {
		action = "output"
		test = "<build>"
	}
	if action == "build-fail" {
		action = "fail"
		test = "<build>"
	}
	key := pkg + "/" + test
	switch action {
	case "output":
		if output == "" {
			return
		}
		p := c.pending[key]
		if p == nil {
			if c.bytes+len(key)+1 > repairFeedbackLimit {
				c.artifact.incomplete = true
				return
			}
			f, err := os.CreateTemp(c.artifact.dir, "observed-*.tmp")
			if err != nil {
				c.artifact.err = err
				return
			}
			if err := f.Close(); err != nil {
				c.artifact.err = err
				return
			}
			p = &feedbackPending{path: f.Name(), pkg: pkg, test: test, bytes: len(key) + 1}
			c.bytes += len(key) + 1
			c.pending[key] = p
		}
		if p.truncated {
			return
		}
		text := p.tail + output
		c.bytes -= len(p.tail)
		p.tail = ""
		cut := max(0, len(text)-c.keep)
		for _, secret := range c.secrets {
			start := max(0, cut-len(secret)+1)
			for {
				i := strings.Index(text[start:], secret)
				if i < 0 {
					break
				}
				i += start
				if i < cut && i+len(secret) > cut {
					cut = i
				}
				start = i + 1
				if start >= len(text) {
					break
				}
			}
		}
		for cut > 0 && cut < len(text) && !utf8.RuneStart(text[cut]) {
			cut--
		}
		if c.bytes+len(text)-cut > repairFeedbackLimit {
			p.truncated = true
			return
		}
		p.tail = text[cut:]
		c.bytes += len(p.tail)
		c.writeOutput(p, text[:cut])

	case "fail", "skip":
		r := feedbackRecord{Kind: "test_failure", Stage: c.stage, Package: pkg, Test: test, Trust: "native_event_observation"}
		if c.probe != nil {
			r.Probe = c.probe(test)
		}
		c.artifact.record(r)
		c.flush(key, true)
	case "pass":
		c.flush(key, false)
	}
}
func (c *feedbackCapture) flush(key string, retain bool) {
	p := c.pending[key]
	if p == nil {
		return
	}
	tail := p.tail
	c.bytes -= len(tail)
	p.tail = ""
	if retain && !p.truncated {
		c.writeOutput(p, tail)
	}
	delete(c.pending, key)
	c.bytes -= p.bytes
	defer func() {
		if err := os.Remove(p.path); err != nil && !os.IsNotExist(err) {
			c.artifact.err = err
		}
	}()
	if !retain {
		return
	}
	if p.truncated {
		c.artifact.incomplete = true
		return
	}
	file, err := os.Open(p.path)
	if err != nil {
		c.artifact.err = err
		return
	}
	defer func() {
		if err := file.Close(); err != nil {
			c.artifact.err = err
		}
	}()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), repairFeedbackLimit)
	for scanner.Scan() {
		c.artifact.append(append(scanner.Bytes(), '\n'))
		c.artifact.records++
	}
	if err := scanner.Err(); err != nil {
		c.artifact.err = err
	}

}
func (c *feedbackCapture) finish(failed bool, pending ...string) {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	for _, fragment := range pending {
		if fragment != "" {
			c.artifact.incomplete = true
		}
	}
	keys := make([]string, 0, len(c.pending))
	for k := range c.pending {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		c.flush(k, failed)
	}
}

type feedbackStream struct {
	capture *feedbackCapture
	name    string
}

func (w feedbackStream) Write(p []byte) (int, error) {
	w.capture.event("output", "", w.name, string(p))
	return len(p), nil
}
func feedbackCommandOutput(ctx context.Context, cmd *exec.Cmd) ([]byte, error) {
	c := newFeedbackCapture(ctx, "command")
	if c == nil {
		return boundedCommandOutput(cmd)
	}
	var output repairOutput
	writer := io.MultiWriter(&output, feedbackStream{c, "combined"})
	cmd.Stdout = writer
	cmd.Stderr = writer
	err := cmd.Run()
	c.finish(err != nil)
	return []byte(output.String()), err
}

func feedbackPrompt(checkpoint string, cp repairCheckpoint, proposal []byte) (string, error) {
	path, err := feedbackPath(checkpoint, cp.Feedback, cp, proposal)
	if err != nil {
		return "", &feedbackArtifactError{err}
	}
	return "Read the complete verification failure records from this JSONL file: " + path, nil
}
func saveFeedbackError(ctx context.Context, checkpoint string, cp *repairCheckpoint, proposal []byte, cause error) error {
	_, a, err := newFeedbackArtifact(ctx, checkpoint, *cp, proposal)
	if err != nil {
		return &feedbackArtifactError{err}
	}
	ref, err := a.seal(cause)
	if err != nil {
		return err
	}
	cp.Feedback = ref
	return nil
}

func (c *feedbackCapture) markIncomplete() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.artifact.incomplete = true
}

// Hash and check JSONL framing incrementally, not by loading the whole artifact.
func readFeedbackFile(path string) (string, feedbackRecord, feedbackRecord, int, error) {
	var header, footer feedbackRecord
	info, err := os.Lstat(path)
	if err != nil {
		return "", header, footer, 0, err
	}
	if !info.Mode().IsRegular() || info.Size() > repairFeedbackLimit {
		return "", header, footer, 0, errors.New("invalid feedback artifact file")
	}
	file, err := os.Open(path)
	if err != nil {
		return "", header, footer, 0, err
	}
	defer file.Close()
	hash := sha256.New()
	scanner := bufio.NewScanner(io.TeeReader(io.LimitReader(file, repairFeedbackLimit+1), hash))
	scanner.Buffer(make([]byte, 64<<10), repairFeedbackLimit)
	count := 0
	var last []byte
	for scanner.Scan() {
		count++
		if count == 1 {
			if err := json.Unmarshal(scanner.Bytes(), &header); err != nil {
				return "", header, footer, count, err
			}
		}
		last = append(last[:0], scanner.Bytes()...)
	}
	if err := scanner.Err(); err != nil {
		return "", header, footer, count, err
	}
	if count < 2 {
		return "", header, footer, count, errors.New("incomplete feedback framing")
	}
	if err := json.Unmarshal(last, &footer); err != nil {
		return "", header, footer, count, err
	}
	return hex.EncodeToString(hash.Sum(nil)), header, footer, count, nil
}

func (c *feedbackCapture) writeOutput(p *feedbackPending, output string) {
	if output == "" || p.truncated {
		return
	}
	b, err := marshalFeedbackRecord(feedbackRecord{Kind: "observed_output", Stage: c.stage, Package: p.pkg, Test: p.test, Trust: "untrusted_process_output", Output: output})
	if err != nil {
		c.artifact.err = err
		return
	}
	b = append(b, '\n')
	if c.bytes+len(b) > repairFeedbackLimit {
		p.truncated = true
		return
	}
	file, err := os.OpenFile(p.path, os.O_WRONLY|os.O_APPEND, 0600)
	if err != nil {
		c.artifact.err = err
		return
	}
	n, err := file.Write(b)
	if closeErr := file.Close(); err == nil {
		err = closeErr
	}
	p.bytes += n
	c.bytes += n
	if err != nil {
		c.artifact.err = err
	} else if n != len(b) {
		c.artifact.err = io.ErrShortWrite
	}
}

// Redact decoded JSON strings, including frozen witnesses and property names;
// replacing already escaped JSON can miss quotes/backslashes in a credential.
// A redacted record is diagnostic evidence, not an exact replayable witness.
func marshalFeedbackRecord(record feedbackRecord) ([]byte, error) {
	raw, err := json.Marshal(record)
	if err != nil {
		return nil, err
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	changed := false
	var scrub func(any) any
	scrub = func(v any) any {
		switch x := v.(type) {
		case json.Number:
			r := redactRepairDiagnostic(x.String())
			if r != x.String() {
				changed = true
				return r
			}
			return x
		case string:
			r := redactRepairDiagnostic(x)
			if r != x {
				changed = true
			}
			return r
		case []any:
			for i, v := range x {
				x[i] = scrub(v)
			}
			return x
		case map[string]any:
			out := make(map[string]any, len(x))
			for key, v := range x {
				newKey := redactRepairDiagnostic(key)
				if newKey != key {
					changed = true
				}
				out[newKey] = scrub(v)
			}
			return out
		default:
			return v
		}
	}
	value = scrub(value)
	if changed {
		value.(map[string]any)["redacted"] = true
	}
	return json.Marshal(value)
}

func discardFeedback(a *feedbackArtifact) error {
	if e := a.file.Close(); e != nil {
		return &feedbackArtifactError{e}
	}
	if e := os.Remove(a.file.Name()); e != nil {
		return &feedbackArtifactError{e}
	}
	if a.err != nil {
		return &feedbackArtifactError{a.err}
	}
	return nil
}
func appendReviewerAnalysis(ctx context.Context, checkpoint string, cp *repairCheckpoint, proposal []byte, analysis string) error {
	path, e := feedbackPath(checkpoint, cp.Feedback, *cp, proposal)
	if e != nil {
		return e
	}
	_, a, e := newFeedbackArtifact(ctx, checkpoint, *cp, proposal)
	if e != nil {
		return e
	}
	file, e := os.Open(path)
	if e != nil {
		discardFeedback(a)
		return e
	}
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64<<10), repairFeedbackLimit)
	for scanner.Scan() {
		var record feedbackRecord
		decoder := json.NewDecoder(bytes.NewReader(scanner.Bytes()))
		decoder.UseNumber()
		if e := decoder.Decode(&record); e != nil {
			a.err = e
			break
		}
		if record.Kind != "header" && record.Kind != "footer" {
			a.record(record)
		}
	}
	if e := scanner.Err(); e != nil {
		a.err = e
	}
	if e := file.Close(); e != nil {
		a.err = e
	}
	a.record(feedbackRecord{Kind: "reviewer_analysis", Trust: "untrusted_reviewer_analysis", Output: analysis})
	ref, e := a.seal(nil)
	if e != nil {
		return e
	}
	cp.Feedback = ref
	return nil
}

// Retain only the checkpoint's explicitly bound artifact, never provisional
// passing output or unrelated host files. Older rounds are not acceptance proof.
func retainFeedback(checkpoint string, ref feedbackRef) error {
	dir := filepath.Join(checkpoint, "feedback")
	entries, e := os.ReadDir(dir)
	if os.IsNotExist(e) {
		return nil
	}
	if e != nil {
		return e
	}
	for _, entry := range entries {
		path := filepath.Join(dir, entry.Name())
		if filepath.Join("feedback", entry.Name()) == ref.File {
			continue
		}
		if entry.IsDir() || entry.Type()&os.ModeSymlink != 0 || (!strings.HasPrefix(entry.Name(), "round-") && !strings.HasPrefix(entry.Name(), "sealed-") && !strings.HasPrefix(entry.Name(), "observed-")) {
			return errors.New("unexpected diagnostic artifact entry")
		}
		if e := os.Remove(path); e != nil {
			return e
		}
	}
	return nil
}

func copyFeedbackFile(source, destination string) (err error) {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	output, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return err
	}
	defer func() {
		if err != nil {
			os.Remove(destination)
		}
	}()
	n, err := io.Copy(output, io.LimitReader(input, repairFeedbackLimit+1))
	if n > repairFeedbackLimit {
		err = errors.New("feedback backup exceeds bound")
	}
	if closeErr := output.Close(); err == nil {
		err = closeErr
	}
	return err
}
