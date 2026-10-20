package protocolsync

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// A normal fast-forward push alone can accept an unproved intermediate main.
// Bind the server advertisement to the proved M without any force option. Git's
// receive-pack compare-and-swap then protects changes after this advertisement.
// This is trusted fixed code in a private host directory, never a candidate hook.
func prepareForwardMainHook(head, main string) (string, func(), error) {
	if !shaRE.MatchString(head) || !shaRE.MatchString(main) {
		return "", nil, errors.New("exact main proof required")
	}
	dir, err := os.MkdirTemp("", "forward-main-proof-")
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
  test "$remote_ref" = refs/heads/main
  test "$remote_oid" = '%s'
  count=$((count + 1))
done
test "$count" -eq 1
`, head, head, main)
	if err = os.WriteFile(filepath.Join(dir, "pre-push"), []byte(body), 0700); err != nil {
		cleanup()
		return "", nil, err
	}
	return dir, cleanup, nil
}
