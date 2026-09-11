package snapshot

import (
	"testing"
	"time"

	sqlitestate "github.com/fwtllh-png/QCode/internal/persist/state/sqlite"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func TestDeliverablePlanPersistsPurposeWithoutExecutionTransitions(t *testing.T) {
	repository, database, content := testRepository(t)
	plan := protocol.SessionPlanArtifact{
		Version: protocol.CheckpointProtocolVersion,
		ID:      "plan-deliverable", SessionID: "session-1", ThreadID: "thread-1",
		TurnID: "turn-1", Cursor: 6, Status: protocol.PlanArtifactReady,
		Purpose: protocol.PlanPurposeDeliverable,
		Body: `{"version":1,"revision":1,"purpose":"deliverable","steps":[` +
			`{"id":"future","title":"Future implementation","status":"pending"}]}`,
		ProfileRevision: 1, CreatedAt: time.Now().UTC(),
	}
	if _, err := repository.SavePlan(t.Context(), plan); err != nil {
		t.Fatal(err)
	}
	var path string
	if err := database.DB().QueryRowContext(t.Context(),
		`SELECT file FROM pragma_database_list WHERE name = 'main'`).Scan(&path); err != nil {
		t.Fatal(err)
	}
	reopened, err := sqlitestate.Open(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = reopened.Close() })
	fresh := NewRepository(reopened.DB(), content)
	recovered, err := fresh.GetPlan(t.Context(), plan.ID)
	if err != nil {
		t.Fatal(err)
	}
	if recovered.Purpose != protocol.PlanPurposeDeliverable ||
		recovered.Body != plan.Body || recovered.CanImplement || recovered.CanAutopilot {
		t.Fatalf("recovered artifact = %+v", recovered)
	}
	latest, found, err := fresh.LatestPlan(t.Context(), plan.SessionID, plan.ThreadID)
	if err != nil || !found || latest.Purpose != protocol.PlanPurposeDeliverable {
		t.Fatalf("latest=%+v found=%t err=%v", latest, found, err)
	}
	plan.CanImplement = true
	if _, err := fresh.SavePlan(t.Context(), plan); err == nil {
		t.Fatal("deliverable gained execution transition")
	}
}
