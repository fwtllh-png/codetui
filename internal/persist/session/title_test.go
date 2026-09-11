package session_test

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwtllh-png/QCode/internal/persist/session"
	sqlitestate "github.com/fwtllh-png/QCode/internal/persist/state/sqlite"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func titleRepository(t *testing.T, source protocol.SessionTitleSource) (*session.Repository, protocol.SessionSummary) {
	t.Helper()
	store, err := sqlitestate.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close() })
	repository := session.NewSQLiteRepository(store)
	summary, err := repository.CreateLifecycle(t.Context(), protocol.SessionCreateSeed{
		Version: 1, SessionID: "session-title", WorkspaceID: "workspace",
		WorkspaceRoot: t.TempDir(), WorkspaceLabel: "test", ThreadID: "thread-title",
		Title: "New Chat", TitleSource: source, Provider: "fixture", Model: "fixture", Isolation: "shared",
	})
	if err != nil {
		t.Fatal(err)
	}
	return repository, summary
}

func TestGeneratedTitleOwnershipAndRevision(t *testing.T) {
	for _, action := range []string{"pin", "manual", "manual same text", "archive", "stale", "delete"} {
		t.Run(action, func(t *testing.T) {
			repository, summary := titleRepository(t, protocol.SessionTitleDefault)
			summary, changed, err := repository.UpdateGeneratedTitle(t.Context(),
				summary.SessionID, summary.ThreadID, summary.TitleRevision,
				protocol.SessionTitleTemporary, "Please inspect the parser")
			if err != nil || !changed {
				t.Fatalf("temporary title: %v %v", changed, err)
			}
			revision := summary.TitleRevision
			title := "My investigation"
			yes := true
			var patch protocol.SessionLifecyclePatch
			switch action {
			case "pin":
				patch.Pinned = &yes
			case "manual", "manual same text":
				if action == "manual same text" {
					title = summary.Title
				}
				patch.Title = &title
			case "archive":
				patch.Archived = &yes
			case "stale":
				revision--
			case "delete":
				_, err = repository.CreateLifecycle(t.Context(), protocol.SessionCreateSeed{
					Version: 1, SessionID: "sibling", WorkspaceID: "workspace",
					WorkspaceRoot: summary.WorkspaceRoot, WorkspaceLabel: "test", ThreadID: "sibling",
					Title: "Sibling", Provider: "fixture", Model: "fixture", Isolation: "shared",
				})
				if err != nil {
					t.Fatal(err)
				}
				_, err = repository.DeleteLifecycle(t.Context(), summary.SessionID, summary.Revision)
			}
			if patch.Title != nil || patch.Pinned != nil || patch.Archived != nil {
				summary, err = repository.UpdateLifecycle(t.Context(), summary.SessionID, summary.Revision, patch)
			}
			if err != nil {
				t.Fatal(err)
			}
			summary, changed, err = repository.UpdateGeneratedTitle(t.Context(),
				"session-title", "thread-title", revision, protocol.SessionTitleAuto, "Inspect parser behavior")
			if action == "delete" {
				if err == nil {
					t.Fatal("deleted session was recreated")
				}
				return
			}
			if err != nil || changed != (action == "pin") {
				t.Fatalf("automatic title: changed=%v err=%v", changed, err)
			}
			if action == "pin" && (!summary.Pinned || summary.Title != "Inspect parser behavior" ||
				summary.TitleSource != protocol.SessionTitleAuto) {
				t.Fatalf("pin invalidated naming: %+v", summary)
			}
			if strings.HasPrefix(action, "manual") &&
				(summary.Title != title || summary.TitleSource != protocol.SessionTitleManual) {
				t.Fatalf("manual title overwritten: %+v", summary)
			}
			reloaded, err := repository.GetLifecycle(t.Context(), summary.SessionID)
			if err != nil || reloaded.TitleSource != summary.TitleSource || reloaded.TitleRevision != summary.TitleRevision {
				t.Fatalf("title ownership did not persist: %+v %v", reloaded, err)
			}
		})
	}
}

func TestExplicitNewChatAndUnknownTitleOwnershipStayManual(t *testing.T) {
	for _, source := range []protocol.SessionTitleSource{"", protocol.SessionTitleManual} {
		repository, summary := titleRepository(t, source)
		if summary.TitleSource != protocol.SessionTitleManual {
			t.Fatal("missing ownership must not be guessed from title text")
		}
		_, changed, err := repository.UpdateGeneratedTitle(t.Context(), summary.SessionID, summary.ThreadID,
			summary.TitleRevision, protocol.SessionTitleTemporary, "Replacement")
		if err != nil || changed {
			t.Fatalf("manual title changed: %v %v", changed, err)
		}
	}
}
