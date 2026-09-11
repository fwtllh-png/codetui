package agentcontext

import (
	"context"

	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

// TurnContextStore keeps an immutable pre-Turn baseline and commits withdrawal
// together with the replacement current context. Neither operation changes usage.
type TurnContextStore interface {
	SaveTurnBaseline(context.Context, protocol.ThreadID, protocol.TurnID, ContextSnapshot) error
	TurnBaseline(context.Context, protocol.ThreadID, protocol.TurnID) (ContextSnapshot, bool, error)
	TurnWithdrawn(context.Context, protocol.ThreadID, protocol.TurnID) (bool, error)
	CommitTurnWithdrawal(context.Context, protocol.ThreadID, protocol.TurnID, ContextSnapshot) error
}
