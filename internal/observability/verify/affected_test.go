package verify

import (
	"path/filepath"
	"strings"
	"testing"
)

func TestRelativePathsAcceptsBothSpellingsOfTheRoot(t *testing.T) {
	root := t.TempDir()
	resolved, err := filepath.EvalSymlinks(root)
	if err != nil {
		t.Fatal(err)
	}
	paths := relativePaths(root, []string{
		filepath.Join(resolved, "a.go"),
		filepath.Join(root, "b.go"),
		"c.go",
	})
	want := []string{"a.go", "b.go", "c.go"}
	if strings.Join(paths, ",") != strings.Join(want, ",") {
		t.Fatalf("relativePaths() = %v, want %v", paths, want)
	}
}
