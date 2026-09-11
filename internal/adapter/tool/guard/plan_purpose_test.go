package guard

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/tool"
	"github.com/fwtllh-png/QCode/internal/adapter/tool/interact"
	"github.com/fwtllh-png/QCode/internal/security/policy"
)

func TestDeliverablePlanDoesNotAuthorizeExecution(t *testing.T) {
	workspace := t.TempDir()
	registry := tool.NewRegistry(nil, nil)
	if err := interact.Register(registry, interact.Options{Workspace: workspace}); err != nil {
		t.Fatal(err)
	}
	write := &testExecutor{descriptor: writeDescriptor()}
	if err := registry.Register(write); err != nil {
		t.Fatal(err)
	}
	runtime := policy.DefaultRuntime(policy.ModeAct, policy.PermissionBypass)
	runtime.ConfigurePlanning(policy.PlanningRequired)
	guard, err := New(Options{Registry: registry, Policy: runtime, Workspace: workspace})
	if err != nil {
		t.Fatal(err)
	}
	for _, purpose := range []string{"deliverable", "execution"} {
		plan, err := json.Marshal(map[string]any{
			"purpose": purpose, "steps": []any{map[string]any{"title": "Edit target"}},
		})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := guard.Execute(t.Context(), "submit-"+purpose, "submit_plan", plan); err != nil {
			t.Fatal(err)
		}
		if purpose == "execution" {
			continue
		}
		if runtime.PlanningSnapshot().PlanSubmitted {
			t.Fatal("deliverable granted execution authority")
		}
		_, err = guard.Execute(t.Context(), "write-with-deliverable", "write",
			json.RawMessage(`{"path":"target.txt","value":"updated"}`))
		var denied *policy.DecisionError
		if !errors.As(err, &denied) || denied.Code != "plan_required" || write.calls.Load() != 0 {
			t.Fatalf("write after deliverable: err=%v calls=%d", err, write.calls.Load())
		}
	}
	// A later deliverable neither grants nor revokes the existing execution plan.
	if _, err := guard.Execute(t.Context(), "submit-later-deliverable", "submit_plan",
		json.RawMessage(`{"purpose":"deliverable","steps":[{"title":"Future task"}]}`)); err != nil {
		t.Fatal(err)
	}
	if !runtime.PlanningSnapshot().PlanSubmitted {
		t.Fatal("deliverable replaced prior execution authority")
	}
	if _, err := guard.Execute(t.Context(), "write-with-execution", "write",
		json.RawMessage(`{"path":"target.txt","value":"updated"}`)); err != nil {
		t.Fatal(err)
	}
	if write.calls.Load() != 1 {
		t.Fatalf("write calls=%d, want 1", write.calls.Load())
	}
}
