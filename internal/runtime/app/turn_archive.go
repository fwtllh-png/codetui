package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	agentcontext "github.com/fwtllh-png/QCode/internal/runtime/agent/context"
	"github.com/fwtllh-png/QCode/internal/runtime/agent/turnkernel"
)

// TurnTranscriptArchive recovers a closed turn's transcript from the turn's
// durable terminal envelope. Each committed envelope carries the encoded
// context envelope whose manifest references the complete staged history in
// the content store, so turns removed from memory by compaction or
// replacement remain recoverable by their persisted turn identity.
type TurnTranscriptArchive struct {
	terminals turnkernel.TerminalEnvelopeStore
	blobs     agentcontext.BlobStore
}

func NewTurnTranscriptArchive(
	terminals turnkernel.TerminalEnvelopeStore,
	blobs agentcontext.BlobStore,
) *TurnTranscriptArchive {
	if terminals == nil || blobs == nil {
		return nil
	}
	return &TurnTranscriptArchive{terminals: terminals, blobs: blobs}
}

// LookupTurn returns the staged history of the turn's own terminal envelope.
// It returns a nil slice when the turn has no committed envelope.
func (a *TurnTranscriptArchive) LookupTurn(
	ctx context.Context,
	turnID string,
) ([]provider.Message, error) {
	if a == nil || turnID == "" {
		return nil, nil
	}
	envelope, _, err := a.terminals.LoadTerminal(ctx, turnID)
	if errors.Is(err, turnkernel.ErrTerminalEnvelopeMissing) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("load terminal envelope: %w", err)
	}
	if len(envelope.SessionDelta) == 0 {
		return nil, nil
	}
	encoded, err := agentcontext.DecodeContextEnvelope(envelope.SessionDelta)
	if err != nil {
		return nil, fmt.Errorf("decode context envelope: %w", err)
	}
	snapshot, err := agentcontext.LoadContextManifest(
		ctx, a.blobs, encoded.Manifest,
	)
	if err != nil {
		return nil, fmt.Errorf("load context manifest: %w", err)
	}
	return snapshot.History, nil
}
