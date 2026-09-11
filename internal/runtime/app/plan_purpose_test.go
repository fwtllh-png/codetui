package app

import (
	"context"
	"testing"
	"time"

	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

type purposeArtifactStore struct {
	memoryArtifactStore
}

func (s *purposeArtifactStore) SavePlan(_ context.Context, plan protocol.SessionPlanArtifact) (protocol.SessionPlanArtifact, error) {
	s.plan = plan
	return plan, nil
}

func TestDeliverablePlanProjectionPersistsArtifactWithoutAuthority(t *testing.T) {
	profile := runtimeTestProfile()
	artifacts := &purposeArtifactStore{}
	runtime := NewRuntime(Options{
		Engine: &profileTestEngine{}, SessionProfiles: &memoryProfileStore{profile: profile},
		DefaultProfile: profile, ProfileCapabilities: runtimeTestCapabilities(profile),
		SessionLifecycle: artifactLifecycle(), SessionArtifacts: artifacts,
	})
	t.Cleanup(func() { closeRuntime(t, runtime) })
	delta := &protocol.PlanDeltaData{
		Done: true, Purpose: protocol.PlanPurposeDeliverable,
		Body: `{"version":1,"purpose":"deliverable","steps":[{"id":"future","title":"Future","status":"pending"}]}`,
	}
	if err := runtime.ArtifactService.DecoratePlanArtifact(t.Context(), "thread-profile", "turn-plan", delta); err != nil {
		t.Fatal(err)
	}
	if delta.ArtifactID == "" || delta.CanImplement || delta.CanAutopilot {
		t.Fatalf("projection = %+v", delta)
	}
	event, err := protocol.NewEvent(protocol.EventMeta{
		Sequence: 1, OperationID: "op-plan", ThreadID: "thread-profile",
		TurnID: "turn-plan", ItemID: "item-plan",
	}, delta)
	if err != nil {
		t.Fatal(err)
	}
	runtime.ArtifactService.PersistSessionArtifact(t.Context(), event)
	if artifacts.plan.Purpose != protocol.PlanPurposeDeliverable ||
		artifacts.plan.Body != delta.Body || artifacts.plan.ExecutionProfileDigest == "" {
		t.Fatalf("persisted artifact = %+v", artifacts.plan)
	}
	if err := artifacts.plan.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestRecoveryDoesNotPromoteDeliverablePlans(t *testing.T) {
	for _, withExecution := range []bool{false, true} {
		name := "deliverable only"
		if withExecution {
			name = "execution then deliverable"
		}
		t.Run(name, func(t *testing.T) {
			events := NewMemoryEventStore(8)
			executionBody := `{"version":1,"revision":1,"steps":[{"id":"current","title":"Current work","status":"pending"}]}`
			deliverableBody := `{"version":1,"revision":1,"purpose":"deliverable","steps":[{"id":"future","title":"Future work","status":"pending"}]}`
			data := []protocol.EventData{&protocol.TurnStartedData{
				Provider: "fixture", Model: "fixture-model", ProfileRevision: 2,
				Intent: protocol.TurnIntentAnswer, Prompt: "Deliver a plan",
			}}
			if withExecution {
				data = append(data, &protocol.PlanDeltaData{
					Body: executionBody, Done: true, ArtifactID: "plan-execution",
					Purpose: protocol.PlanPurposeExecution, ProfileRevision: 2,
					Status: string(protocol.PlanArtifactReady), CanImplement: true, CanAutopilot: true,
				})
			}
			data = append(data, &protocol.PlanDeltaData{
				Body: deliverableBody, Done: true, ArtifactID: "plan-deliverable",
				Purpose: protocol.PlanPurposeDeliverable, ProfileRevision: 2,
				Status: string(protocol.PlanArtifactReady),
			}, &protocol.ExecutionReceiptData{
				Goal: "Deliver a plan", Intent: protocol.TurnIntentAnswer,
				// Even a nonempty execution receipt must not authorize the deliverable.
				Plan: executionBody, Verification: protocol.ReceiptVerification{},
			}, &protocol.TurnFailedData{
				Code: protocol.CodeUnavailable, Message: "interrupted",
			})
			for index, item := range data {
				event, err := protocol.NewEvent(protocol.EventMeta{
					Sequence: protocol.Cursor(index + 1), OperationID: "operation-plan",
					ThreadID: "thread-profile", TurnID: "turn-plan", ItemID: "item-plan",
				}, item)
				if err != nil {
					t.Fatal(err)
				}
				if err := events.Append(t.Context(), event); err != nil {
					t.Fatal(err)
				}
			}
			profile := runtimeTestProfile()
			profile.Revision = 2
			runtime := NewRuntime(Options{
				Engine: &profileTestEngine{}, EventStore: events,
				SessionProfiles: &memoryProfileStore{profile: profile},
				DefaultProfile:  profile, ProfileCapabilities: runtimeTestCapabilities(profile),
				SessionLifecycle: artifactLifecycle(),
			})
			t.Cleanup(func() { closeRuntime(t, runtime) })
			for _, action := range []protocol.TurnRecoveryAction{protocol.TurnRecoveryContinue, protocol.TurnRecoveryRetry} {
				prepared, err := runtime.PrepareTurnRecovery(t.Context(), protocol.TurnRecoveryRequest{
					Version: protocol.WorkflowIntentVersion, Action: action,
					SessionID: "session-profile", SourceTurnID: "turn-plan",
					IdempotencyKey: "recover-" + string(action),
				})
				if err != nil {
					t.Fatal(err)
				}
				want := ""
				if withExecution {
					want = "plan-execution"
				}
				if prepared.Recovery.PlanID != want {
					t.Fatalf("recovery=%+v want PlanID=%q", prepared.Recovery, want)
				}
			}
			forged := &protocol.StartTurnPayload{
				ThreadID: "thread-profile",
				Recovery: &protocol.TurnRecoveryContext{
					Action: protocol.TurnRecoveryContinue, SourceTurnID: "turn-plan",
					PlanID: "plan-deliverable", PlanTransition: protocol.PlanTransitionAutopilot,
					ProfileRevision: 2,
				},
			}
			if err := runtime.PrepareStartPayload(t.Context(), "/workspace", forged); protocol.CodeOf(err) != protocol.CodeConflict {
				t.Fatalf("deliverable recovery authorization: %v", err)
			}
		})
	}
}

func TestDeliverableArtifactCannotStartPlanExecution(t *testing.T) {
	profile := runtimeTestProfile()
	artifacts := &memoryArtifactStore{plan: protocol.SessionPlanArtifact{
		Version: protocol.CheckpointProtocolVersion,
		ID:      "plan-deliverable", SessionID: "session-profile",
		ThreadID: "thread-profile", TurnID: "turn-plan", Cursor: 7,
		Status: protocol.PlanArtifactReady, Purpose: protocol.PlanPurposeDeliverable,
		Body: `{"version":1,"revision":1,"purpose":"deliverable","steps":[` +
			`{"id":"future","title":"Future work","status":"pending"}]}`,
		ProfileRevision: profile.Revision, CreatedAt: time.Now().UTC(),
	}}
	runtime := NewRuntime(Options{
		Engine: &profileTestEngine{}, SessionProfiles: &memoryProfileStore{profile: profile},
		DefaultProfile: profile, ProfileCapabilities: runtimeTestCapabilities(profile),
		SessionLifecycle: artifactLifecycle(), SessionArtifacts: artifacts,
	})
	t.Cleanup(func() { closeRuntime(t, runtime) })
	snapshot, err := runtime.SessionPlan(t.Context(), "session-profile")
	if err != nil || snapshot.Artifact == nil || snapshot.Artifact.Purpose != protocol.PlanPurposeDeliverable {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	for _, transition := range []protocol.PlanTransition{protocol.PlanTransitionImplement, protocol.PlanTransitionAutopilot} {
		if _, err := runtime.PreparePlanExecution(t.Context(), "session-profile", "plan-deliverable", transition); protocol.CodeOf(err) != protocol.CodeConflict {
			t.Fatalf("deliverable %s: %v", transition, err)
		}
		if _, err := runtime.PreparePlanExecutionTo(t.Context(), "session-profile", "target-session", "plan-deliverable", transition); protocol.CodeOf(err) != protocol.CodeConflict {
			t.Fatalf("deliverable cross-session %s: %v", transition, err)
		}
	}
}
