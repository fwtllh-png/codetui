package turnkernel

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/tool"
)

func TestTurnDiffTracksNetExistenceAndCanonicalPaths(t *testing.T) {
	root := t.TempDir()
	tracker := NewTurnDiffTracker(root)
	tracker.Record(TurnDiffEntry{Path: filepath.Join(root, "new.go"), Kind: "created", Added: 4})
	tracker.Record(TurnDiffEntry{Path: "./new.go", Kind: "modified", Added: 2, Removed: 1})
	got := tracker.Snapshot()
	if len(got) != 1 || got[0].Path != "new.go" || got[0].Kind != "created" ||
		got[0].Added != 6 || got[0].Removed != 1 {
		t.Fatalf("created then modified: %+v", got)
	}
	tracker.Record(TurnDiffEntry{Path: "new.go", Kind: "deleted"})
	if len(tracker.Snapshot()) != 0 {
		t.Fatal("created then deleted remained in net diff")
	}
	tracker.Record(TurnDiffEntry{Path: "existing.go", Kind: "deleted"})
	tracker.Record(TurnDiffEntry{Path: "existing.go", Kind: "created"})
	if got := tracker.Snapshot(); len(got) != 1 || got[0].Kind != "modified" {
		t.Fatalf("replacement: %+v", got)
	}
}

func TestTurnDiffTrackerRecordAndFormat(t *testing.T) {
	t.Parallel()
	tracker := NewTurnDiffTracker()
	tracker.Record(TurnDiffEntry{Path: "b.txt", Tool: "file_write", Kind: "created"})
	tracker.Record(TurnDiffEntry{Path: "a.txt", Tool: "file_edit", Kind: "modified"})
	tracker.Record(TurnDiffEntry{Path: "b.txt", Tool: "file_patch", Kind: "modified"})
	got := tracker.Format()
	for _, want := range []string{"a.txt", "b.txt", "file_patch", "modified"} {
		if !strings.Contains(got, want) {
			t.Fatalf("format = %q, want it to mention %q", got, want)
		}
	}
	if strings.Contains(got, "file_write") {
		t.Fatalf("format = %q, want the later write to win for b.txt", got)
	}
	tracker.Reset()
	if tracker.Format() != "" {
		t.Fatal("expected empty after reset")
	}
}

func TestObservedFileChangesReadsTypedFacts(t *testing.T) {
	t.Parallel()
	if got := ObservedFileChanges(tool.Result{}); got != nil {
		t.Fatalf("changes from empty result = %+v", got)
	}
	result := tool.Result{Outcome: &tool.Outcome{Facts: &tool.OutcomeFacts{
		WorkspaceChanges: []tool.WorkspaceChange{
			{Path: "a.txt", Kind: tool.WorkspaceCreated},
		},
	}}}
	got := ObservedFileChanges(result)
	if len(got) != 1 || got[0].Path != "a.txt" || got[0].Kind != tool.WorkspaceCreated {
		t.Fatalf("changes = %+v", got)
	}
}
