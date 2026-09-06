package engine

import (
	"context"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	"github.com/fwtllh-png/QCode/internal/adapter/tool"
)

type archiveFixture struct {
	turnID  string
	history []provider.Message
	calls   int
}

func (a *archiveFixture) LookupTurn(
	_ context.Context, turnID string,
) ([]provider.Message, error) {
	a.calls++
	if turnID != a.turnID {
		return nil, nil
	}
	return a.history, nil
}

func TestLookupTurnHistoryFallsBackToArchive(t *testing.T) {
	runtime := &scriptedProvider{streams: []provider.Stream{
		textStream("ok"),
	}}
	engine := newEngine(t, runtime, tool.NewRegistry(nil, nil))
	archive := &archiveFixture{
		turnID: "turn-archived",
		history: []provider.Message{
			messageWithText(provider.RoleUser, "archived question", 7),
			messageWithText(provider.RoleAssistant, "archived answer", 7),
		},
	}
	engine.options.TurnTranscriptArchive = archive
	engine.turnIDs["turn-archived"] = 7
	// The archived turn was removed from the in-memory history by a
	// replacement that only retains the current turn.
	engine.history = []provider.Message{
		messageWithText(provider.RoleUser, "current request", 8),
	}

	messages, err := engine.lookupTurnHistory(t.Context(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if len(messages) != 2 || messages[0].Text() != "archived question" {
		t.Fatalf("archived messages = %+v", messages)
	}
	if archive.calls != 1 {
		t.Fatalf("archive calls = %d", archive.calls)
	}

	// In-memory hits never consult the archive.
	messages, err = engine.lookupTurnHistory(t.Context(), 8)
	if err != nil || len(messages) != 1 {
		t.Fatalf("current lookup = %+v err=%v", messages, err)
	}
	if archive.calls != 1 {
		t.Fatalf("archive consulted for in-memory turn: %d", archive.calls)
	}

	// Unknown turn numbers stay honest misses instead of probing storage.
	if messages, err = engine.lookupTurnHistory(t.Context(), 9); err != nil ||
		len(messages) != 0 {
		t.Fatalf("unknown lookup = %+v err=%v", messages, err)
	}
	if archive.calls != 1 {
		t.Fatalf("archive consulted for unknown turn: %d", archive.calls)
	}
}
