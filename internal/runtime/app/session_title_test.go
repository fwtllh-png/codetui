package app

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/fwtllh-png/QCode/internal/persist/session"
	sqlitestate "github.com/fwtllh-png/QCode/internal/persist/state/sqlite"
	agentengine "github.com/fwtllh-png/QCode/internal/runtime/agent/engine"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

type titleTestEngine struct {
	Engine
	started chan string
	release chan struct{}
	fail    bool
}

func (e *titleTestEngine) GenerateSessionTitle(ctx context.Context, _ protocol.ThreadID, _, prompt string) (agentengine.SessionTitleResult, error) {
	e.started <- prompt
	select {
	case <-ctx.Done():
		return agentengine.SessionTitleResult{}, ctx.Err()
	case <-e.release:
	}
	if e.fail {
		return agentengine.SessionTitleResult{}, errors.New("unavailable")
	}
	return agentengine.SessionTitleResult{Title: "Improve session naming"}, nil
}

func TestSessionTitleAsyncLifecycle(t *testing.T) {
	for _, scenario := range []string{"success", "manual wins", "model failure", "blocked", "canceled", "shutdown"} {
		t.Run(scenario, func(t *testing.T) {
			state, err := sqlitestate.Open(t.Context(), filepath.Join(t.TempDir(), "state.db"))
			if err != nil {
				t.Fatal(err)
			}
			defer state.Close()
			repository := session.NewSQLiteRepository(state)
			summary, err := repository.CreateLifecycle(t.Context(), protocol.SessionCreateSeed{
				Version: 1, SessionID: "session-title", WorkspaceID: "workspace",
				WorkspaceRoot: t.TempDir(), WorkspaceLabel: "test", ThreadID: "thread-title",
				Title: "New Chat", TitleSource: protocol.SessionTitleDefault,
				Provider: "fixture", Model: "fixture", Isolation: "shared",
			})
			if err != nil {
				t.Fatal(err)
			}
			engine := &titleTestEngine{
				started: make(chan string, 1), release: make(chan struct{}), fail: scenario == "model failure",
			}
			runtime := NewRuntime(Options{SessionLifecycle: repository, Engine: engine})
			defer runtime.Close(context.Background())
			start := &protocol.StartTurnPayload{
				ThreadID: summary.ThreadID, TurnID: "turn-title", ItemID: "item-title",
				Prompt: "internal expanded prompt", DisplayPrompt: "Please improve how session titles are named",
			}
			operation := protocol.Operation{ID: "op-title", Kind: protocol.OperationStartTurn, Payload: start}
			runtime.prepareSessionTitle(t.Context(), summary, operation, start)
			initial, err := repository.GetLifecycle(t.Context(), summary.SessionID)
			if err != nil || initial.Title != "New Chat" ||
				initial.TitleSource != protocol.SessionTitleDefault {
				t.Fatalf("prompt must not fill title=%+v err=%v", initial, err)
			}
			select {
			case prompt := <-engine.started:
				if prompt != start.DisplayPrompt {
					t.Fatal("title request used internal prompt expansion")
				}
			case <-t.Context().Done():
				t.Fatal("title generation waited for turn completion")
			}
			runtime.prepareSessionTitle(t.Context(), initial, operation, start)
			if scenario == "shutdown" {
				if err := runtime.Close(t.Context()); err != nil {
					t.Fatal(err)
				}
				return
			}
			if scenario == "blocked" || scenario == "canceled" {
				var terminal protocol.EventData = &protocol.TurnFailedData{
					Code: protocol.CodeUnavailable, Message: "Work remains",
				}
				if scenario == "canceled" {
					terminal = &protocol.TurnCanceledData{Reason: protocol.CancelReasonUserInterrupted}
				}
				if err := runtime.publish(operation.ID, start.ThreadID, start.TurnID, start.ItemID, terminal); err != nil {
					t.Fatal(err)
				}
			}
			if scenario == "manual wins" {
				title := "My title"
				if _, err := runtime.UpdateSessionLifecycle(t.Context(), summary.SessionID,
					initial.Revision, protocol.SessionLifecyclePatch{Title: &title}); err != nil {
					t.Fatal(err)
				}
			}
			close(engine.release)
			runtime.titleWorkers.Wait()
			current, err := repository.GetLifecycle(t.Context(), summary.SessionID)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]string{
				"success": "Improve session naming", "manual wins": "My title",
				"model failure": "New Chat", "blocked": "Improve session naming", "canceled": "Improve session naming",
			}[scenario]
			if current.Title != want {
				t.Fatalf("title=%q want=%q", current.Title, want)
			}
			events, err := runtime.events.Replay(t.Context(), 0)
			if err != nil {
				t.Fatal(err)
			}
			auto := 0
			for _, event := range events {
				if data, ok := event.Data.(*protocol.SessionTitleUpdatedData); ok &&
					data.TitleSource == protocol.SessionTitleAuto {
					auto++
				}
			}
			if (auto == 1) != (scenario == "success" || scenario == "blocked" || scenario == "canceled") {
				t.Fatalf("automatic title events=%d", auto)
			}
			// An accepted prompt cannot be used to rename this session again.
			runtime.prepareSessionTitle(t.Context(), current, operation, start)
			if len(engine.started) != 0 {
				t.Fatal("later prompt retriggered naming")
			}
			if scenario == "model failure" {
				engine.fail = false
				next := *start
				next.TurnID = "turn-retry-title"
				nextOperation := operation
				nextOperation.Payload = &next
				runtime.prepareSessionTitle(t.Context(), current, nextOperation, &next)
				runtime.titleWorkers.Wait()
				current, err = repository.GetLifecycle(t.Context(), summary.SessionID)
				if err != nil || current.TitleSource != protocol.SessionTitleAuto {
					t.Fatalf("later turn did not retry naming: %+v %v", current, err)
				}
			}
		})
	}
}
