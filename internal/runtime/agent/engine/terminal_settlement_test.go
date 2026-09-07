package engine

import (
	"context"
	"encoding/json"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	"github.com/fwtllh-png/QCode/internal/adapter/tool"
	agentcontext "github.com/fwtllh-png/QCode/internal/runtime/agent/context"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

type interruptingProbe struct {
	engine *Engine
	calls  atomic.Int32
}

func (p *interruptingProbe) Descriptor() tool.Descriptor {
	return tool.Descriptor{
		Name: "interrupt_probe", Description: "requests cancellation while running",
		Visibility: tool.VisibleModel, Capability: tool.CapabilityRead,
		AccessMode: tool.AccessRead, ParallelPolicy: tool.ParallelConcurrent,
		SandboxRequirement: tool.SandboxNone, Availability: tool.AvailabilityAvailable,
		InputSchema: map[string]any{
			"type":       "object",
			"properties": map[string]any{"text": map[string]any{"type": "string"}},
			"required":   []string{"text"}, "additionalProperties": false,
		},
	}
}

func (p *interruptingProbe) Execute(
	_ context.Context,
	_ json.RawMessage,
) (tool.Result, error) {
	p.calls.Add(1)
	if control, err := p.engine.Control(); err == nil {
		_ = control.Cancel(protocol.CancelReasonUserInterrupted)
	}
	return tool.Result{Content: "probe output"}, nil
}

// A cancellation accepted while tools are running must settle the closed
// conversation through the terminal envelope: the completed result was
// already published to the host, so losing it from the durable history
// would force the next Turn to repeat the exploration.
func TestAcceptedCancellationDuringToolsSettlesClosedExchanges(t *testing.T) {
	registry := tool.NewRegistry(nil, nil)
	probe := &interruptingProbe{}
	if err := registry.Register(probe); err != nil {
		t.Fatal(err)
	}
	runtime := &scriptedProvider{streams: []provider.Stream{
		toolCallStream("call-1", "interrupt_probe", `{"text":"probe"}`),
	}}
	engine := newEngine(t, runtime, registry)
	probe.engine = engine

	var states []State
	result, err := engine.Run(t.Context(), "inspect the probe", func(event Event) error {
		states = append(states, event.State)
		return nil
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if result.State != Canceled {
		t.Fatalf("result state = %v, want %v", result.State, Canceled)
	}
	assertOneTerminal(t, states, Canceled)
	if probe.calls.Load() != 1 {
		t.Fatalf("probe executions = %d, want exactly one", probe.calls.Load())
	}
	history := engine.History()
	if !historyPairsClosedWithCall(t, history, "call-1", "probe output") ||
		!historyContainsText(history, "inspect the probe") {
		t.Fatalf("canceled turn history = %+v", history)
	}
	if revision := engine.SessionRevision(); revision != 1 {
		t.Fatalf("session revision = %d, want 1", revision)
	}
}

func TestRetainedFailedTurnExchangesDropsOrphansKeepsClosed(t *testing.T) {
	transaction := []provider.Message{
		messageWithText(provider.RoleUser, "inspect web", 1),
		toolCallMessage(1, "paired", "read", `{}`),
		toolResultMessage(1, "paired", "result"),
		toolCallMessage(2, "prior", "read", `{}`),
		toolCallMessage(1, "orphan", "read", `{}`),
		messageWithText(provider.RoleAssistant, "closed conclusion", 1),
	}
	retained := retainedFailedTurnExchanges(transaction, 1)
	if len(retained) != 4 ||
		retained[0].Text() != "inspect web" ||
		retained[1].Blocks[0].ToolCall.ID != "paired" ||
		retained[2].Blocks[0].ToolResult.CallID != "paired" ||
		retained[3].Text() != "closed conclusion" {
		t.Fatalf("retained failed exchanges = %+v", retained)
	}
}

// A Turn that explored successfully and then failed on a later sample must
// keep its closed exchanges plus the failure note instead of collapsing to
// the pre-Turn history.
func TestFailedTurnRetainsClosedExchanges(t *testing.T) {
	registry := tool.NewRegistry(nil, nil)
	if err := registry.Register(&echoTool{}); err != nil {
		t.Fatal(err)
	}
	runtime := &scriptedProvider{streams: []provider.Stream{
		toolCallStream("call-1", "echo", `{"text":"probe"}`),
	}}
	engine := newEngine(t, runtime, registry)

	var states []State
	result, err := engine.Run(t.Context(), "inspect the parser", func(event Event) error {
		states = append(states, event.State)
		return nil
	})
	if err == nil {
		t.Fatal("Run() succeeded, want provider failure")
	}
	if result.State != Failed {
		t.Fatalf("result state = %v, want %v", result.State, Failed)
	}
	assertOneTerminal(t, states, Failed)
	history := engine.History()
	if !historyPairsClosedWithCall(t, history, "call-1", "probe") ||
		!historyContainsText(history, "inspect the parser") {
		t.Fatalf("failed turn history lost closed exchanges: %+v", history)
	}
	var sawFailureNote bool
	for _, message := range history {
		if message.Role == provider.RoleSystem &&
			strings.HasPrefix(message.Text(), "[turn_terminal]") {
			sawFailureNote = true
		}
	}
	if !sawFailureNote {
		t.Fatalf("failed turn history lost the failure note: %+v", history)
	}
	if revision := engine.SessionRevision(); revision != 1 {
		t.Fatalf("session revision = %d, want 1", revision)
	}
}

// The recovery acceptance center is the first model request after Continue:
// it must carry the completed tool evidence from the canceled source Turn
// instead of forcing the model to re-explore.
func TestContinueAfterAcceptedCancellationSeesCompletedResults(t *testing.T) {
	registry := tool.NewRegistry(nil, nil)
	probe := &interruptingProbe{}
	if err := registry.Register(probe); err != nil {
		t.Fatal(err)
	}
	engine := newEngine(
		t,
		&scriptedProvider{streams: []provider.Stream{
			toolCallStream("call-1", "interrupt_probe", `{"text":"probe"}`),
		}},
		registry,
	)
	probe.engine = engine

	var states []State
	result, err := engine.Run(t.Context(), "inspect the probe", func(event Event) error {
		states = append(states, event.State)
		return nil
	})
	if err != nil || result.State != Canceled {
		t.Fatalf("Run() = (%v, %v), want canceled turn", result.State, err)
	}
	assertOneTerminal(t, states, Canceled)
	var sourceTurnID protocol.TurnID
	for turnID := range engine.historyTurns {
		sourceTurnID = protocol.TurnID(turnID)
	}
	if sourceTurnID == "" {
		t.Fatal("canceled turn left no durable history binding")
	}

	continued := &scriptedProvider{streams: []provider.Stream{
		textStream("final"),
	}}
	engine.options.Provider = continued
	if _, err := engine.RunForTurnWithRequest(
		t.Context(),
		"turn-continue",
		TurnRequest{
			Prompt: "continue the inspection",
			Intent: protocol.TurnIntentAnswer,
			Recovery: &protocol.TurnRecoveryContext{
				Action:       protocol.TurnRecoveryContinue,
				SourceTurnID: sourceTurnID,
			},
		},
		nil,
	); err != nil {
		t.Fatal(err)
	}
	if len(continued.requests) != 1 {
		t.Fatalf("provider requests = %d", len(continued.requests))
	}
	request := continued.requests[0]
	if !requestContains(request, "inspect the probe") ||
		!requestContainsToolResult(request, "probe output") {
		t.Fatalf("continue request lost completed evidence: %+v", request.Messages)
	}
}

func requestContainsToolResult(
	request provider.ModelRequest,
	part string,
) bool {
	for _, message := range request.Messages {
		for _, block := range message.Blocks {
			if block.ToolResult != nil &&
				strings.Contains(block.ToolResult.Content, part) {
				return true
			}
		}
	}
	return false
}

func historyContainsText(history []provider.Message, text string) bool {
	for _, message := range history {
		if message.Text() == text {
			return true
		}
	}
	return false
}

func historyPairsClosedWithCall(
	t *testing.T,
	history []provider.Message,
	callID string,
	resultPart string,
) bool {
	t.Helper()
	sawCall, sawResult := false, false
	for _, message := range history {
		for _, block := range message.Blocks {
			if block.ToolCall != nil && block.ToolCall.ID == callID {
				sawCall = true
			}
			if block.ToolResult != nil && block.ToolResult.CallID == callID &&
				strings.Contains(block.ToolResult.Content, resultPart) {
				sawResult = true
			}
		}
	}
	if !sawCall || !sawResult {
		return false
	}
	return agentcontext.ToolPairsClosed(history)
}
