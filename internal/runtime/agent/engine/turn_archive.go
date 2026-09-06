package engine

import (
	"context"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
)

// TurnTranscriptArchive recovers the durable transcript of a closed turn by
// its persisted turn identity. The runtime implements it over terminal
// envelopes and their staged context manifests; implementations return a nil
// slice when no durable transcript exists for the turn.
type TurnTranscriptArchive interface {
	LookupTurn(ctx context.Context, turnID string) ([]provider.Message, error)
}
