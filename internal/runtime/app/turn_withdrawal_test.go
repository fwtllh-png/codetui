package app

import (
	"context"
	"errors"
	"testing"

	agentcontext "github.com/fwtllh-png/QCode/internal/runtime/agent/context"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

type appWithdrawalStore struct {
	artifactCurrentContextStore
	withdrawn bool
}

func (*appWithdrawalStore) SaveTurnBaseline(context.Context, protocol.ThreadID, protocol.TurnID, agentcontext.ContextSnapshot) error {
	return nil
}
func (*appWithdrawalStore) TurnBaseline(context.Context, protocol.ThreadID, protocol.TurnID) (agentcontext.ContextSnapshot, bool, error) {
	return agentcontext.ContextSnapshot{}, true, nil
}
func (s *appWithdrawalStore) TurnWithdrawn(context.Context, protocol.ThreadID, protocol.TurnID) (bool, error) {
	return s.withdrawn, nil
}
func (s *appWithdrawalStore) CommitTurnWithdrawal(context.Context, protocol.ThreadID, protocol.TurnID, agentcontext.ContextSnapshot) error {
	s.withdrawn = true
	return nil
}

type appWithdrawalEngine struct {
	testEngine
	store   *appWithdrawalStore
	enter   chan struct{}
	release chan struct{}
}

func (e *appWithdrawalEngine) WithdrawTurn(context.Context, protocol.ThreadID, protocol.TurnID) error {
	if e.enter != nil {
		close(e.enter)
		<-e.release
	}
	e.store.withdrawn = true
	return nil
}

func TestWithdrawLatestTurnAdmissionRecoveryAndPublication(t *testing.T) {
	lifecycle := artifactLifecycle()
	lifecycle.summary.LatestTurnID = "latest"
	store := &appWithdrawalStore{}
	engine := &appWithdrawalEngine{store: store, enter: make(chan struct{}), release: make(chan struct{})}
	runtime := NewRuntime(Options{Engine: engine, SessionLifecycle: lifecycle, ContextRebaseStore: store})
	t.Cleanup(func() { closeRuntime(t, runtime) })
	request := protocol.TurnWithdrawRequest{SessionID: "session-profile", TurnID: "older"}
	if err := runtime.WithdrawTurn(t.Context(), request); protocol.CodeOf(err) != protocol.CodeConflict {
		t.Fatalf("older Turn: %v", err)
	}
	request.TurnID = "latest"
	done := make(chan error, 1)
	go func() { done <- runtime.WithdrawTurn(t.Context(), request) }()
	<-engine.enter
	operation, err := protocol.NewOperation(&protocol.StartTurnPayload{
		ThreadID: "thread-profile", TurnID: "next", ItemID: "item", Prompt: "next",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := runtime.Submit(t.Context(), operation); protocol.CodeOf(err) != protocol.CodeConflict {
		t.Fatalf("new work admitted: %v", err)
	}
	if _, err := runtime.beginWorkspaceOperation(); protocol.CodeOf(err) != protocol.CodeConflict {
		t.Fatalf("Git operation admitted: %v", err)
	}
	close(engine.release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	summary, err := runtime.SessionStatus(t.Context(), request.SessionID)
	if err != nil || !summary.LatestTurnWithdrawn || summary.Status != protocol.SessionStatusIdle {
		t.Fatalf("summary: %+v %v", summary, err)
	}
	if err := runtime.requireRetainedTurn(t.Context(), "thread-profile", "latest"); protocol.CodeOf(err) != protocol.CodeConflict {
		t.Fatal("withdrawn Turn recoverable")
	}
	if _, err := runtime.PrepareTurnRecovery(t.Context(), protocol.TurnRecoveryRequest{
		Version: protocol.WorkflowIntentVersion, SessionID: request.SessionID, SourceTurnID: "latest",
		Action: protocol.TurnRecoveryContinue, IdempotencyKey: "continue",
	}); protocol.CodeOf(err) != protocol.CodeConflict {
		t.Fatalf("Continue accepted: %v", err)
	}
	engine.enter = nil
	if err := runtime.WithdrawTurn(t.Context(), request); err != nil {
		t.Fatal(err)
	}
	events, err := runtime.events.Replay(t.Context(), 0)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, event := range events {
		if event.Kind == protocol.EventTurnWithdrawn {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("withdrawal event count = %d", count)
	}
}

func TestWithdrawTurnWaitIsCancelable(t *testing.T) {
	lifecycle := artifactLifecycle()
	lifecycle.summary.LatestTurnID = "latest"
	store := &appWithdrawalStore{}
	runtime := NewRuntime(Options{Engine: &appWithdrawalEngine{store: store}, SessionLifecycle: lifecycle, ContextRebaseStore: store})
	t.Cleanup(func() { closeRuntime(t, runtime) })
	operation, err := protocol.NewOperation(&protocol.StartTurnPayload{
		ThreadID: "thread-profile", TurnID: "latest", ItemID: "item", Prompt: "prompt",
	})
	if err != nil {
		t.Fatal(err)
	}
	canonical, err := CanonicalOperationPayload(operation)
	if err != nil {
		t.Fatal(err)
	}
	runtime.OperationService.mu.Lock()
	runtime.accepted[operation.ID] = PendingOperation{ID: operation.ID, Canonical: canonical}
	runtime.OperationService.mu.Unlock()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := runtime.WithdrawTurn(ctx, protocol.TurnWithdrawRequest{SessionID: "session-profile", TurnID: "latest"}); !errors.Is(err, context.Canceled) {
		t.Fatalf("wait: %v", err)
	}
	if store.withdrawn {
		t.Fatal("canceled wait mutated context")
	}
	runtime.commitLocal(operation.ID)
}
