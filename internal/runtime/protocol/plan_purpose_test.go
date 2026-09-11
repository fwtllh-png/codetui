package protocol

import (
	"encoding/json"
	"testing"
	"time"
)

func TestDeliverablePlanArtifactAndEventContracts(t *testing.T) {
	body := `{"version":1,"purpose":"deliverable","revision":1,"steps":[{"id":"future","title":"Future work","status":"pending"}]}`
	artifact := SessionPlanArtifact{
		Version: CheckpointProtocolVersion,
		ID:      "plan-1", SessionID: "session-1", ThreadID: "thread-1",
		TurnID: "turn-1", Cursor: 1, ProfileRevision: 1,
		Status: PlanArtifactReady, Purpose: PlanPurposeDeliverable, Body: body,
		CreatedAt: time.Now().UTC(),
	}
	delta := PlanDeltaData{
		Body: body, Purpose: PlanPurposeDeliverable, Done: true,
		ArtifactID: "plan-1", ProfileRevision: 1, Status: string(PlanArtifactReady),
	}
	for _, test := range []struct {
		name         string
		purpose      PlanPurpose
		canImplement bool
		canAutopilot bool
		valid        bool
	}{
		{"deliverable", PlanPurposeDeliverable, false, false, true},
		{"cannot implement", PlanPurposeDeliverable, true, false, false},
		{"cannot autopilot", PlanPurposeDeliverable, false, true, false},
		{"cannot relabel", PlanPurposeExecution, true, true, false},
		{"cannot omit purpose", "", true, true, false},
		{"unknown purpose", "unknown", false, false, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			artifact.Purpose, artifact.CanImplement, artifact.CanAutopilot =
				test.purpose, test.canImplement, test.canAutopilot
			delta.Purpose, delta.CanImplement, delta.CanAutopilot =
				test.purpose, test.canImplement, test.canAutopilot
			if err := artifact.Validate(); (err == nil) != test.valid {
				t.Fatalf("artifact err=%v want valid=%t", err, test.valid)
			}
			if err := delta.validate(); (err == nil) != test.valid {
				t.Fatalf("event err=%v want valid=%t", err, test.valid)
			}
		})
	}
}

func TestPlanPurposeRoundTripAndDefault(t *testing.T) {
	for _, purpose := range []PlanPurpose{"", PlanPurposeExecution, PlanPurposeDeliverable} {
		body, err := json.Marshal(struct {
			Purpose PlanPurpose `json:"purpose,omitempty"`
		}{purpose})
		if err != nil {
			t.Fatal(err)
		}
		actual, err := PlanPurposeFromBody(string(body))
		if err != nil || actual != purpose.Normalize() {
			t.Fatalf("purpose=%q actual=%q err=%v", purpose, actual, err)
		}
	}
	for _, body := range []string{`{"purpose":"unknown"}`, `{"purpose":1}`, `invalid`} {
		if _, err := PlanPurposeFromBody(body); err == nil {
			t.Fatalf("invalid purpose accepted: %s", body)
		}
	}
}
