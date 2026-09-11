package web_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	contract "github.com/fwtllh-png/QCode/internal/host/runtimeapi/runtimecontract"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func TestWebWithdrawsLatestRunningTurn(t *testing.T) {
	fixture, err := filepath.Abs("../../../../testdata/providers/slow")
	if err != nil {
		t.Fatal(err)
	}
	host := newWebContractHost(t, contract.Setup{Fixture: fixture}).(*webContractHost)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	live, err := host.Live(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := host.StartTurn(ctx, "mistaken request")
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event := <-live:
			if event.TurnID == turn.TurnID && event.Kind == protocol.EventTurnStarted {
				goto started
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
started:
	var result struct{}
	request := protocol.TurnWithdrawRequest{SessionID: host.sessionID, TurnID: turn.TurnID}
	if err := host.call(ctx, "turn/withdraw", request, &result, "withdraw"); err != nil {
		t.Fatal(err)
	}
	if err := host.call(ctx, "turn/withdraw", request, &result, "withdraw-again"); err != nil {
		t.Fatal(err)
	}
	var summary protocol.SessionSummary
	if err := host.call(ctx, "session/status", map[string]string{"session_id": host.sessionID}, &summary, ""); err != nil {
		t.Fatal(err)
	}
	if !summary.LatestTurnWithdrawn || summary.Status != protocol.SessionStatusIdle {
		t.Fatalf("summary: %+v", summary)
	}
	if _, err := host.RecoverTurn(ctx, turn.TurnID, protocol.TurnRecoveryContinue, ""); err == nil {
		t.Fatal("withdrawn Turn accepted Continue")
	}
	next, err := host.StartTurn(ctx, "a new request after withdrawal")
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event := <-live:
			if event.TurnID != next.TurnID {
				continue
			}
			if event.Kind == protocol.EventTurnStarted {
				return
			}
			if event.Kind == protocol.EventOperationRejected || event.Kind == protocol.EventTurnFailed {
				t.Fatalf("new Turn rejected after withdrawal: %+v", event.Data)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}

func TestWebImmediateStopPreservesBaselineAndRetry(t *testing.T) {
	fixture, err := filepath.Abs("../../../../testdata/providers/slow")
	if err != nil {
		t.Fatal(err)
	}
	host := newWebContractHost(t, contract.Setup{Fixture: fixture}).(*webContractHost)
	ctx, cancel := context.WithTimeout(t.Context(), 20*time.Second)
	defer cancel()
	live, err := host.Live(ctx, 0)
	if err != nil {
		t.Fatal(err)
	}
	turn, err := host.StartTurn(ctx, "stop immediately")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := host.Cancel(ctx, turn, protocol.CancelReasonUserInterrupted); err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event := <-live:
			if event.TurnID == turn.TurnID && protocol.IsTerminalEvent(event.Kind) {
				goto stopped
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
stopped:
	retried, err := host.RecoverTurn(ctx, turn.TurnID, protocol.TurnRecoveryRetry, "")
	if err != nil {
		t.Fatal(err)
	}
	for {
		select {
		case event := <-live:
			if event.TurnID != retried.TurnID {
				continue
			}
			if event.Kind == protocol.EventTurnStarted {
				return
			}
			if event.Kind == protocol.EventOperationRejected || event.Kind == protocol.EventTurnFailed {
				t.Fatalf("Retry rejected: %+v", event.Data)
			}
		case <-ctx.Done():
			t.Fatal(ctx.Err())
		}
	}
}
