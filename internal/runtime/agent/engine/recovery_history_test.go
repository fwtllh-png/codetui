package engine

import (
	"slices"
	"strings"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	agentcontext "github.com/fwtllh-png/QCode/internal/runtime/agent/context"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func recoveryEnvelopeText(goal string, sourceTurnID string) string {
	return goal + "\n\n<recovery_evidence>{\"v\":2}</recovery_evidence>" +
		"\n<source_request turn=\"" + sourceTurnID + "\"/>"
}

func TestRecoveryHistoryKeepsContinueExchangesAndDedupesEnvelope(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, nil)
	engine.history = []provider.Message{
		messageWithText(provider.RoleUser, "earlier request", 1),
		messageWithText(provider.RoleAssistant, "earlier answer", 1),
		messageWithText(
			provider.RoleUser,
			recoveryEnvelopeText("inspect the parser", "turn-origin"),
			2,
		),
		toolCallMessage(2, "call-1", "file_read", `{"path":"a.go"}`),
		toolResultMessage(2, "call-1", "package a"),
		messageWithText(provider.RoleAssistant, "unfinished", 2),
	}
	agentcontext.ReconcileHistoryTurns(&engine.historyTurns, engine.history, "turn-source", 2)

	history := agentcontext.RecoveryBaseHistory(engine.history, engine.historyTurns, &protocol.TurnRecoveryContext{
		Action:       protocol.TurnRecoveryContinue,
		SourceTurnID: "turn-source",
	})
	if len(history) != 5 ||
		history[0].Text() != "earlier request" ||
		history[1].Text() != "earlier answer" ||
		history[2].Blocks[0].ToolCall.ID != "call-1" ||
		history[3].Blocks[0].ToolResult.CallID != "call-1" ||
		history[4].Text() != "unfinished" {
		t.Fatalf("continue base history = %+v", history)
	}
	for _, message := range history {
		if message.Text() == "inspect the parser" {
			t.Fatalf("continue base history kept the source envelope: %+v", history)
		}
	}
	if !agentcontext.ToolPairsClosed(history) {
		t.Fatalf("continue base history broke tool pairs: %+v", history)
	}
	if len(engine.history) != 6 {
		t.Fatalf("recovery projection mutated durable history: %+v", engine.history)
	}
}

func TestRecoveryHistoryReplacesSourceTurnForRetry(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, nil)
	engine.history = []provider.Message{
		messageWithText(provider.RoleUser, "earlier request", 1),
		messageWithText(provider.RoleAssistant, "earlier answer", 1),
		messageWithText(provider.RoleUser, "retry me", 2),
		toolCallMessage(2, "call-1", "file_read", `{"path":"a.go"}`),
		toolResultMessage(2, "call-1", "package a"),
	}
	agentcontext.ReconcileHistoryTurns(&engine.historyTurns, engine.history, "turn-source", 2)

	history := agentcontext.RecoveryBaseHistory(engine.history, engine.historyTurns, &protocol.TurnRecoveryContext{
		Action:       protocol.TurnRecoveryRetry,
		SourceTurnID: "turn-source",
	})
	if len(history) != 2 ||
		history[0].Text() != "earlier request" ||
		history[1].Text() != "earlier answer" {
		t.Fatalf("retry base history = %+v", history)
	}
	if !agentcontext.ToolPairsClosed(history) {
		t.Fatalf("retry base history broke tool pairs: %+v", history)
	}
}

func TestRepeatedRecoveryDoesNotAccumulateRecoveryTurns(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, nil)
	engine.history = []provider.Message{
		messageWithText(provider.RoleUser, "earlier request", 1),
		messageWithText(provider.RoleAssistant, "earlier answer", 1),
		messageWithText(
			provider.RoleUser,
			recoveryEnvelopeText("inspect the parser", "turn-origin"),
			2,
		),
		messageWithText(provider.RoleAssistant, "partial one", 2),
	}
	agentcontext.ReconcileHistoryTurns(&engine.historyTurns, engine.history, "recovery-1", 2)

	second := agentcontext.RecoveryBaseHistory(engine.history, engine.historyTurns, &protocol.TurnRecoveryContext{
		Action:       protocol.TurnRecoveryContinue,
		SourceTurnID: "recovery-1",
	})
	second = append(
		second,
		messageWithText(
			provider.RoleUser,
			recoveryEnvelopeText("inspect the parser", "recovery-1"),
			3,
		),
		messageWithText(provider.RoleAssistant, "partial two", 3),
	)
	engine.history = cloneMessages(second)
	agentcontext.ReconcileHistoryTurns(&engine.historyTurns, engine.history, "recovery-2", 3)

	third := agentcontext.RecoveryBaseHistory(engine.history, engine.historyTurns, &protocol.TurnRecoveryContext{
		Action:       protocol.TurnRecoveryContinue,
		SourceTurnID: "recovery-2",
	})
	if len(third) != 4 ||
		third[0].Text() != "earlier request" ||
		third[1].Text() != "earlier answer" ||
		third[2].Text() != "partial one" ||
		third[3].Text() != "partial two" {
		t.Fatalf("third recovery lost accumulated exchanges: %+v", third)
	}
	for _, message := range third {
		if text := message.Text(); text != "" &&
			strings.Contains(text, "<recovery_evidence>") {
			t.Fatalf("third recovery accumulated prior envelopes: %+v", third)
		}
	}
}

func TestRecoveryRunProjectsOnlyCanonicalCurrentEnvelope(t *testing.T) {
	runtime := &scriptedProvider{streams: []provider.Stream{
		textStream("continued"),
	}}
	engine := newEngine(t, runtime, nil)
	engine.history = []provider.Message{
		messageWithText(provider.RoleUser, "earlier request", 1),
		messageWithText(provider.RoleAssistant, "earlier answer", 1),
		messageWithText(
			provider.RoleUser,
			recoveryEnvelopeText("stale recovery goal", "turn-source"),
			2,
		),
		messageWithText(provider.RoleAssistant, "stale partial output", 2),
	}
	agentcontext.ReconcileHistoryTurns(&engine.historyTurns, engine.history, "turn-source", 2)

	_, err := engine.RunForTurnWithRequest(
		t.Context(),
		"turn-current",
		TurnRequest{
			Prompt: "canonical current recovery envelope",
			Intent: protocol.TurnIntentAnswer,
			Recovery: &protocol.TurnRecoveryContext{
				Action:       protocol.TurnRecoveryContinue,
				SourceTurnID: "turn-source",
			},
		},
		nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if len(runtime.requests) != 1 {
		t.Fatalf("provider requests = %d", len(runtime.requests))
	}
	var texts []string
	for _, message := range runtime.requests[0].Messages {
		if text := message.Text(); text != "" {
			texts = append(texts, text)
		}
	}
	for _, text := range texts {
		if strings.Contains(text, "<recovery_evidence>") ||
			text == "stale recovery goal" {
			t.Fatalf("provider request retained stale recovery envelope: %q", texts)
		}
	}
	if !slices.Contains(texts, "earlier request") ||
		!slices.Contains(texts, "stale partial output") ||
		!slices.Contains(texts, "canonical current recovery envelope") {
		t.Fatalf("provider request texts = %q", texts)
	}
}

func TestHistoryCompactionReconcilesRecoveryBindings(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, nil)
	engine.history = []provider.Message{
		messageWithText(provider.RoleSystem, "source", 2),
		messageWithText(provider.RoleAssistant, "partial", 2),
	}
	agentcontext.ReconcileHistoryTurns(&engine.historyTurns, engine.history, "turn-source", 2)
	if engine.historyTurns["turn-source"] != 2 {
		t.Fatalf("source binding = %+v", engine.historyTurns)
	}

	engine.ReplaceHistory([]provider.Message{
		messageWithText(provider.RoleSystem, "authority summary", 0),
	})
	if _, ok := engine.historyTurns["turn-source"]; ok {
		t.Fatalf(
			"compaction retained a binding for an absent turn: %+v",
			engine.historyTurns,
		)
	}
	history := agentcontext.RecoveryBaseHistory(engine.history, engine.historyTurns, &protocol.TurnRecoveryContext{
		Action:       protocol.TurnRecoveryContinue,
		SourceTurnID: "turn-source",
	})
	if len(history) != 1 || history[0].Text() != "authority summary" {
		t.Fatalf("compacted recovery history = %+v", history)
	}
}
