package wire

import (
	"context"

	"github.com/fwtllh-png/QCode/internal/adapter/tool/guard"
	"github.com/fwtllh-png/QCode/internal/runtime/app"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
	"github.com/fwtllh-png/QCode/internal/security/policy"
)

func buildGitControl(state *buildState) *app.GitControl {
	if state.security.runtime == nil {
		return nil
	}
	base := state.security.guardFactory
	return &app.GitControl{
		Workspace: state.session.workspaceQuery,
		NewGuard: func(ctx context.Context, profile protocol.SessionProfile) (*guard.Guard, error) {
			factory := base
			factory.runtime = base.runtime.CloneSampling()
			// Operator consent is confined to this request, never an Agent-session grant.
			factory.runtime.Approvals = policy.NewApprovalCache()
			if profile.Version != 0 {
				factory.runtime.Mode = policy.Mode(profile.Mode)
				factory.runtime.Permission = policy.Permission(profile.ApprovalPosture)
			}
			factory.permissions = nil
			return factory.Build(ctx)
		},
	}
}
