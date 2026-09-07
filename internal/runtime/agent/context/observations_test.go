package agentcontext

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWorkspaceRelativeResolvesAlternateRootAliasesAndDeletedPaths(t *testing.T) {
	root := t.TempDir()
	aliases := t.TempDir()
	workspace := filepath.Join(aliases, "workspace")
	alternate := filepath.Join(aliases, "alternate")
	for _, alias := range []string{workspace, alternate} {
		if err := os.Symlink(root, alias); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "present.go"), []byte("package fixture"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path, want string
		ok               bool
	}{
		{"relative", "present.go", "present.go", true},
		{"workspace alias", filepath.Join(workspace, "present.go"), "present.go", true},
		{"other alias", filepath.Join(alternate, "present.go"), "present.go", true},
		{"missing file", filepath.Join(alternate, "deleted.go"), "deleted.go", true},
		{"missing directory", filepath.Join(alternate, "deleted", "file.go"), "deleted/file.go", true},
		{"root itself", alternate, "", false},
		{"outside", filepath.Join(aliases, "outside.go"), "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			got, ok := WorkspaceRelative(workspace, test.path)
			if got != test.want || ok != test.ok {
				t.Fatalf("WorkspaceRelative(%q, %q) = %q, %t; want %q, %t",
					workspace, test.path, got, ok, test.want, test.ok)
			}
		})
	}
}
