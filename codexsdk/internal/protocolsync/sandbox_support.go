package protocolsync

import (
	"os"
	"strings"
)

func redactRepairDiagnostic(s string) string {
	for _, entry := range os.Environ() {
		name, value, _ := strings.Cut(entry, "=")
		if len(value) >= 8 && (strings.Contains(name, "KEY") || strings.Contains(name, "TOKEN") || strings.Contains(name, "SECRET") || name == "CODEX_RESPONSES_API_ENDPOINT") {
			s = strings.ReplaceAll(s, value, "[redacted]")
		}
	}

	return s
}

func boundedRepairDiagnostic(s string) string {
	s = redactRepairDiagnostic(s)
	if len(s) > 8192 {
		s = s[len(s)-8192:]
	}
	return s
}
func repairEnvironment() []string {
	var env []string
	for _, name := range []string{"PATH", "HOME", "GOPATH", "GOCACHE", "GOMODCACHE", "GOTOOLCHAIN", "GOROOT", "TMPDIR", "TMP", "TEMP", "CI"} {
		if value, ok := os.LookupEnv(name); ok {
			env = append(env, name+"="+value)
		}
	}
	return env
}
