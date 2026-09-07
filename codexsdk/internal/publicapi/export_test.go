package publicapi

import (
	"go/token"
	"go/types"
	"strings"
	"testing"
)

func TestPublicObjectStringMasksOnlyStructsWithoutExportedFields(t *testing.T) {
	pkg := types.NewPackage("example.com/inventory", "inventory")
	hidden := types.NewField(token.NoPos, pkg, "hidden", types.Typ[types.String], false)
	exported := types.NewField(token.NoPos, pkg, "Visible", types.Typ[types.String], false)

	opaqueName := types.NewTypeName(token.NoPos, pkg, "Opaque", nil)
	types.NewNamed(opaqueName, types.NewStruct([]*types.Var{hidden}, nil), nil)
	if got := publicObjectString(opaqueName, nil); !strings.Contains(got, "unexported fields") {
		t.Fatalf("opaque struct inventory = %q, want masked private layout", got)
	}

	publicName := types.NewTypeName(token.NoPos, pkg, "Public", nil)
	types.NewNamed(publicName, types.NewStruct([]*types.Var{hidden, exported}, nil), nil)
	got := publicObjectString(publicName, nil)
	if !strings.Contains(got, "Visible string") || strings.Contains(got, "unexported fields") {
		t.Fatalf("public struct inventory = %q, want exported field retained", got)
	}
}
