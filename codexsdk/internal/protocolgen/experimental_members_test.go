package protocolgen

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateExperimentalMembersMatchesCheckedInOutput(t *testing.T) {
	manifest, err := LoadManifest(filepath.Join("..", "protocolschema", "appserver", "v2", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateExperimentalMembers(manifest)
	if err != nil {
		t.Fatal(err)
	}
	generatedAgain, err := GenerateExperimentalMembers(manifest)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, generatedAgain) {
		t.Fatal("generated experimental members are not reproducible")
	}
	checkedIn, err := os.ReadFile(filepath.Join("..", "..", "protocolv2", "experimental_members.gen.go"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(generated, checkedIn) {
		t.Fatal("generated experimental members do not match checked-in protocolv2/experimental_members.gen.go")
	}
}

func TestGenerateExperimentalMembersFollowsClassifiedSurface(t *testing.T) {
	manifest, err := LoadManifest(filepath.Join("..", "protocolschema", "appserver", "v2", "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	generated, err := GenerateExperimentalMembers(manifest)
	if err != nil {
		t.Fatal(err)
	}
	resume := experimentalOwnerBlock(generated, "ThreadResumeParams")
	if resume == "" {
		t.Fatal("missing ThreadResumeParams experimental JSON fields")
	}
	if bytes.Contains([]byte(resume), []byte(`"excludeTurns"`)) {
		t.Fatal("classified-stable ThreadResumeParams.excludeTurns was generated as experimental")
	}
	for _, field := range []string{"history", "path", "permissions", "runtimeWorkspaceRoots"} {
		if !bytes.Contains([]byte(resume), []byte(`"`+field+`"`)) {
			t.Fatalf("classified experimental ThreadResumeParams.%s missing", field)
		}
	}
}

func experimentalOwnerBlock(generated []byte, owner string) string {
	marker := []byte("\t\"" + owner + "\": {\n")
	start := bytes.Index(generated, marker)
	if start < 0 {
		return ""
	}
	rest := generated[start:]
	end := bytes.Index(rest, []byte("\t},\n"))
	if end < 0 {
		return string(rest)
	}
	return string(rest[:end])
}
