package engine

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	"github.com/fwtllh-png/QCode/internal/adapter/tool"
	completiontool "github.com/fwtllh-png/QCode/internal/adapter/tool/completion"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func TestConvergenceFinalizationRepairsRejectedDeclaration(t *testing.T) {
	invalid, err := json.Marshal(map[string]any{
		"status": "incomplete", "summary": "work remains",
		"pending_actions": []string{strings.Repeat("x", 513)},
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime := &scriptedProvider{streams: []provider.Stream{
		textStream("done without changing"),
		textStream("still done without changing"),
		toolCallStream("invalid", completiontool.Name, string(invalid)),
		toolCallStream("corrected", completiontool.Name,
			`{"status":"incomplete","summary":"work remains","pending_actions":["Finish the requested change."]}`),
	}}
	registry := tool.NewRegistry(nil, nil)
	if err := registry.Register(&completiontool.Tool{}); err != nil {
		t.Fatal(err)
	}
	engine := newEngine(t, runtime, registry)
	engine.options.MaxSteps = 1
	var terminal Event
	_, err = engine.RunForTurnWithIntentAndAttachments(
		t.Context(), "turn-schema-repair", "change it",
		protocol.TurnIntentWorkspaceChange, nil, func(event Event) error {
			if event.State == Failed {
				terminal = event
			}
			return nil
		},
	)
	if err == nil || len(runtime.requests) != 4 || terminal.Convergence == nil ||
		len(terminal.Convergence.PendingActions) != 1 ||
		terminal.Convergence.PendingActions[0] != "Finish the requested change." {
		t.Fatalf("err=%v requests=%d convergence=%+v", err, len(runtime.requests), terminal.Convergence)
	}
}
