package protocolsync

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// A normal fast-forward push alone can accept an unproved intermediate main.
// Bind the server advertisement to the proved M without any force option. Git's
// receive-pack compare-and-swap then protects changes after this advertisement.
// This is trusted fixed code in a private host directory, never a candidate hook.
func prepareForwardMainHook(head, main string) (string, func(), error) {
	return prepareExactRefHook(head, main, "main")
}

// prepareExactRefHook binds one ordinary push to the exact server-advertised
// previous head, including an otherwise fast-forwardable intermediate ref.
func prepareExactRefHook(head, previous, branch string) (string, func(), error) {
	if !shaRE.MatchString(head) || !shaRE.MatchString(previous) || branch == "" || exec.Command("git", "check-ref-format", "refs/heads/"+branch).Run() != nil {
		return "", nil, errors.New("exact ref proof required")
	}
	dir, err := os.MkdirTemp("", "exact-ref-proof-")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	body := fmt.Sprintf(`#!/bin/sh
set -eu
count=0
while read -r local_ref local_oid remote_ref remote_oid; do
  test "$local_ref" = '%s'
  test "$local_oid" = '%s'
  test "$remote_ref" = '%s'
  test "$remote_oid" = '%s'
  count=$((count + 1))
done
test "$count" -eq 1
`, head, head, strings.ReplaceAll("refs/heads/"+branch, "'", "'\"'\"'"), previous)
	if err = os.WriteFile(filepath.Join(dir, "pre-push"), []byte(body), 0700); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}
