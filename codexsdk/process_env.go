package codexsdk

import (
	"errors"
	"runtime"
	"strings"
)

// ErrInvalidEnvironment reports malformed child environment entries. It never
// includes entry names or values, which may contain credentials.
var ErrInvalidEnvironment = errors.New("codexsdk: invalid child environment")

func normalizeChildEnvironment(env []string, windows bool) ([]string, error) {
	if env == nil {
		return nil, nil
	}
	normalized := make([]string, 0, len(env))
	positions := make(map[string]int, len(env))
	for _, entry := range env {
		if strings.ContainsRune(entry, '\x00') {
			return nil, ErrInvalidEnvironment
		}
		start := 0
		// Windows environment blocks may contain hidden drive-current-directory
		// entries such as =C:=C:\path. Match os/exec's separator rule for those.
		if windows && strings.HasPrefix(entry, "=") {
			start = 1
		}
		separator := strings.IndexByte(entry[start:], '=')
		if separator < 0 {
			return nil, ErrInvalidEnvironment
		}
		separator += start
		if separator == 0 {
			return nil, ErrInvalidEnvironment
		}
		key := entry[:separator]
		if windows {
			key = strings.ToLower(key)
		}
		if index, ok := positions[key]; ok {
			normalized[index] = entry // os/exec semantics: the last value wins.
		} else {
			positions[key] = len(normalized)
			normalized = append(normalized, entry)
		}
	}
	return normalized, nil
}

func childEnvironment(env []string) ([]string, error) {
	return normalizeChildEnvironment(env, runtime.GOOS == "windows")
}
