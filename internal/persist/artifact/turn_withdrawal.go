package artifact

import (
	"context"

	agentcontext "github.com/fwtllh-png/QCode/internal/runtime/agent/context"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func (r *Service) requireRetainedSource(ctx context.Context, thread protocol.ThreadID, turn protocol.TurnID) error {
	store, ok := r.ContextRebaseStore().(agentcontext.TurnContextStore)
	if !ok {
		return nil
	}
	withdrawn, err := store.TurnWithdrawn(ctx, thread, turn)
	if err != nil {
		return err
	}
	if withdrawn {
		return runtimeProblem(protocol.CodeConflict, "Source Turn was withdrawn", nil)
	}
	return nil
}

func (r *Service) beginContextMutation() (func(), error) {
	if owner, ok := r.ArtifactRuntime.(interface{ BeginContextMutation() (func(), error) }); ok {
		return owner.BeginContextMutation()
	}
	return func() {}, nil
}
