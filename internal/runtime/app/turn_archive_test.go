package app

import (
	"context"
	"errors"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	agentcontext "github.com/fwtllh-png/QCode/internal/runtime/agent/context"
	"github.com/fwtllh-png/QCode/internal/runtime/agent/turnkernel"
)

type memBlobs struct{ values map[string][]byte }

func (b *memBlobs) Put(
	_ context.Context, handle string, data []byte,
) error {
	b.values[handle] = append([]byte(nil), data...)
	return nil
}

func (b *memBlobs) Get(_ context.Context, handle string) ([]byte, error) {
	data, ok := b.values[handle]
	if !ok {
		return nil, errors.New("missing blob " + handle)
	}
	return append([]byte(nil), data...), nil
}

type stubTerminalEnvelopeStore struct {
	envelopes map[string]turnkernel.TerminalEnvelope
}

func (s *stubTerminalEnvelopeStore) AppendDomainFacts(
	context.Context, string, uint64, []turnkernel.DomainFact,
) error {
	return nil
}

func (s *stubTerminalEnvelopeStore) LoadDomainFacts(
	context.Context, string,
) ([]turnkernel.DomainFact, error) {
	return nil, nil
}

func (s *stubTerminalEnvelopeStore) CommitTerminal(
	context.Context, turnkernel.TerminalEnvelope,
) (turnkernel.TerminalCommitMarker, error) {
	return turnkernel.TerminalCommitMarker{}, nil
}

func (s *stubTerminalEnvelopeStore) LoadTerminal(
	_ context.Context, turnID string,
) (turnkernel.TerminalEnvelope, turnkernel.TerminalCommitMarker, error) {
	envelope, ok := s.envelopes[turnID]
	if !ok {
		return turnkernel.TerminalEnvelope{}, turnkernel.TerminalCommitMarker{},
			turnkernel.ErrTerminalEnvelopeMissing
	}
	return envelope, turnkernel.TerminalCommitMarker{}, nil
}

func (s *stubTerminalEnvelopeStore) PendingOutbox(
	context.Context, string,
) ([]turnkernel.ProjectionOutboxEntry, error) {
	return nil, nil
}

func (s *stubTerminalEnvelopeStore) MarkOutboxPublished(
	context.Context, string, []string,
) error {
	return nil
}

func TestTurnTranscriptArchiveRecoversStagedHistory(t *testing.T) {
	blobs := &memBlobs{values: map[string][]byte{}}
	window, err := agentcontext.NewWindowLedger("window-2", 2)
	if err != nil {
		t.Fatal(err)
	}
	binding := agentcontext.WorkspaceBinding{
		WorkspaceIdentity: "workspace:test",
		JournalRevision:   1,
	}
	binding.Seal()
	question := provider.TextMessage(provider.RoleUser, "archived question")
	question.Turn = 3
	answer := provider.TextMessage(provider.RoleAssistant, "archived answer")
	answer.Turn = 3
	snapshot := agentcontext.ContextSnapshot{
		Version: agentcontext.ContextSnapshotVersion,
		Epoch:   1, Revision: 2, Turn: 3,
		History:   []provider.Message{question, answer},
		Workspace: binding, Window: window,
	}
	if err := snapshot.Seal(); err != nil {
		t.Fatal(err)
	}
	manifest, err := agentcontext.BuildContextManifest(
		t.Context(), blobs, "thread-1", "turn-3",
		snapshot, nil, agentcontext.DefaultManifestLimits(),
	)
	if err != nil {
		t.Fatal(err)
	}
	accounting := agentcontext.AccountingDelta{TurnID: "turn-3"}
	accounting.Seal()
	encoded, err := agentcontext.EncodeContextEnvelope(
		manifest, accounting,
	)
	if err != nil {
		t.Fatal(err)
	}
	terminals := &stubTerminalEnvelopeStore{
		envelopes: map[string]turnkernel.TerminalEnvelope{
			"turn-3": {TurnID: "turn-3", SessionDelta: encoded},
		},
	}
	archive := NewTurnTranscriptArchive(terminals, blobs)

	history, err := archive.LookupTurn(t.Context(), "turn-3")
	if err != nil {
		t.Fatal(err)
	}
	if len(history) != 2 ||
		history[0].Text() != "archived question" ||
		history[1].Text() != "archived answer" {
		t.Fatalf("recovered history = %+v", history)
	}

	missing, err := archive.LookupTurn(t.Context(), "turn-unknown")
	if err != nil || missing != nil {
		t.Fatalf("unknown turn = %+v err=%v", missing, err)
	}
	if NewTurnTranscriptArchive(nil, blobs) != nil {
		t.Fatal("archive built without terminal store")
	}
	if NewTurnTranscriptArchive(terminals, nil) != nil {
		t.Fatal("archive built without blob store")
	}
}
