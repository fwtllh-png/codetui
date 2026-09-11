package workspacejournal

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/fwtllh-png/QCode/internal/persist/contentstore"
)

func TestKeepWithdrawnDraftPreservesFilesAndReleasesAdmission(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "value.txt")
	if err := os.WriteFile(path, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := New(root, contentstore.NewMemory(contentstore.Options{}))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Begin("turn"); err != nil {
		t.Fatal(err)
	}
	write(t, manager, path, "kept")
	if err := manager.Suspend("turn"); err != nil {
		t.Fatal(err)
	}
	if err := manager.KeepDraft("turn"); err != nil {
		t.Fatal(err)
	}
	if err := manager.KeepDraft("turn"); err != nil {
		t.Fatal(err)
	}
	if manager.HasDraft("turn") {
		t.Fatal("withdrawn draft still blocks new work")
	}
	if data, _ := os.ReadFile(path); string(data) != "kept" {
		t.Fatal("file was changed")
	}
	if err := manager.Begin("next"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Commit("next"); err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(t.Context()); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "kept" {
		t.Fatal("close rolled back kept changes")
	}
}
