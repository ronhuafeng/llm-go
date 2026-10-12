package protocolsync

import (
	"sort"
	"strings"
)

// Test output is provisional until its terminal event. Passing test logs must
// never displace the first failed assertion. These text buffers total less than
// the existing 64 KiB command-output bound; the rendered result fits within the
// 8 KiB human-facing summary. Full execution records use the caller's writer.
type oracleDiagnostic struct {
	tests              map[string]string
	failures, fallback string
	omitted            bool
	extraFailures      bool
}

func diagnosticPrefix(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const mark = "...[truncated]"
	return s[:limit-len(mark)] + mark
}
func diagnosticTail(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return "...[truncated]" + s[len(s)-(limit-len("...[truncated]")):]
}
func diagnosticContext(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	const mark = "...[truncated]"
	n := (limit - len(mark)) / 2
	return s[:n] + mark + s[len(s)-(limit-len(mark)-n):]
}
func diagnosticStatus(s string) bool {
	s = strings.TrimSpace(s)
	for _, prefix := range []string{"=== RUN ", "=== PAUSE ", "=== CONT ", "=== NAME ", "--- PASS:", "--- FAIL:", "--- SKIP:"} {
		if strings.HasPrefix(s, prefix) {
			return true
		}
	}
	return false
}
func diagnosticContent(output string) string {
	var b strings.Builder
	for output != "" {
		line, rest, hasNewline := strings.Cut(output, "\n")
		if !diagnosticStatus(line) {
			b.WriteString(line)
			if hasNewline {
				b.WriteByte('\n')
			}
		}
		output = rest
	}
	return b.String()
}
func (d *oracleDiagnostic) event(action, pkg, test, output string) {
	key := pkg + "/" + test
	switch action {
	case "build-output":
		d.event("output", pkg, "<build>", output)
	case "build-fail":
		d.event("fail", pkg, "<build>", "")
	case "output":
		output = diagnosticContent(output)
		if output == "" {
			return
		}
		output = redactRepairDiagnostic(output)
		if test == "" {
			d.fallback = diagnosticContext(d.fallback+output, 1024)
			return
		}
		// Bound active logging tests and their keys, even with adversarial events.
		if len(key) > 512 {
			d.omitted = true
			return
		}
		if d.tests == nil {
			d.tests = map[string]string{}
		}
		if _, exists := d.tests[key]; !exists && len(d.tests) >= 12 {
			d.omitted = true
			return
		}
		// Keep the latest assertion after earlier t.Log output, while bounding one
		// enormous output event before joining it to the per-test buffer.
		d.tests[key] = diagnosticTail(d.tests[key]+diagnosticPrefix(output, 4096), 4096)
	case "fail":
		if test == "" {
			return
		} // Package status is not a second assertion.
		text := d.tests[key]
		if text == "" {
			text = "failed without captured assertion output (output may exceed diagnostic bounds)\n"
		}
		label := diagnosticPrefix(redactRepairDiagnostic(key), 512)
		block := label + ":\n" + diagnosticTail(text, 4096-len(label)-2)
		if len(d.failures)+len(block) <= 4096 {
			d.failures += block
		} else {
			d.extraFailures = true
		}
		delete(d.tests, key)
	case "pass", "skip":
		delete(d.tests, key)
	}
}

// Write receives process stderr, not a test verdict. It remains fallback context
// and cannot overwrite an already captured failed-test or compiler diagnostic.
func (d *oracleDiagnostic) Write(p []byte) (int, error) {
	d.fallback = diagnosticContext(d.fallback+redactRepairDiagnostic(string(p)), 1024)
	return len(p), nil
}
func (d *oracleDiagnostic) String() string {
	note := ""
	if d.omitted {
		note = "[diagnostic output omitted: active-test/key bound]\n"
	}
	if d.extraFailures {
		note += "[additional failed-test output omitted at diagnostic bound]\n"
	}
	if d.failures != "" {
		return note + d.fallback + d.failures
	}
	// A process can crash/exit before terminal TestEvents. Keep bounded unfinished
	// context, clearly labelled; only the existing event verifier decides success.
	names := make([]string, 0, len(d.tests))
	for key := range d.tests {
		names = append(names, key)
	}
	sort.Strings(names)
	pending := ""
	for _, key := range names {
		label := "unfinished test output " + redactRepairDiagnostic(key) + ":\n"
		room := 4096 - len(pending) - len(label)
		if room < 32 {
			note += "[additional unfinished-test output omitted at diagnostic bound]\n"
			break
		}
		pending += label + diagnosticTail(d.tests[key], room)
	}
	return note + d.fallback + pending
}
