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
// A Turn that crashed before its terminal commit has no envelope delta; its
// durable continuation snapshot is the remaining transcript source, so the
// archive falls back to it before reporting no history.
func (a *TurnTranscriptArchive) LookupTurn(
	ctx context.Context,
	turnID string,
) ([]provider.Message, error) {
	if a == nil || turnID == "" {
		return nil, nil
	}
	envelope, _, err := a.terminals.LoadTerminal(ctx, turnID)
	if err != nil && !errors.Is(err, turnkernel.ErrTerminalEnvelopeMissing) {
		return nil, fmt.Errorf("load terminal envelope: %w", err)
	}
	if err == nil && len(envelope.SessionDelta) != 0 {
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
	return a.lookupContinuationTranscript(ctx, turnID)
}

// lookupContinuationTranscript recovers the in-turn conversation a crashed
// Turn durably accepted. The kernel facts name the newest continuation
// cursor; its CAS record carries the accepted messages.
func (a *TurnTranscriptArchive) lookupContinuationTranscript(
	ctx context.Context,
	turnID string,
) ([]provider.Message, error) {
	facts, err := a.terminals.LoadDomainFacts(ctx, turnID)
	if err != nil {
		return nil, fmt.Errorf("load continuation facts: %w", err)
	}
	var cursor *turnkernel.ContinuationCursor
	for index := range facts {
		if facts[index].State.Continuation != nil {
			cursor = facts[index].State.Continuation
		}
	}
	if cursor == nil {
		return nil, nil
	}
	record, err := agentcontext.LoadTurnContinuation(
		ctx, a.blobs, cursor.Handle, cursor.Digest,
	)
	if err != nil {
		return nil, fmt.Errorf("load turn continuation: %w", err)
	}
	return record.Messages, nil
}
