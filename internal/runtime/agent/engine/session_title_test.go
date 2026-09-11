package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/fwtllh-png/QCode/internal/adapter/model"
	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	providerfixture "github.com/fwtllh-png/QCode/internal/adapter/provider/fixture"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func TestSessionTitleUsesIsolatedSummaryRequest(t *testing.T) {
	for _, test := range []struct {
		name, text string
		want       string
		stop       provider.StopReason
		valid      bool
	}{
		{name: "valid", text: `{"title":"Improve session naming"}`, valid: true},
		{name: "maximum escaped JSON", text: `{"title":"` + strings.Repeat(`\u0061`, protocol.SessionTitleMaxBytes) + `"}`,
			want: strings.Repeat("a", protocol.SessionTitleMaxBytes), valid: true},
		{name: "oversize JSON", text: strings.Repeat(" ", protocol.SessionTitleJSONMaxBytes+1)},
		{name: "not json", text: "Summary"},
		{name: "extra fields", text: `{"title":"Summary","tool":"exec"}`},
		{name: "trailing json", text: `{"title":"Summary"} {}`},
		{name: "multiline", text: `{"title":"Line one\nLine two"}`},
		{name: "empty", text: `{"title":" "}`},
		{name: "incomplete", text: `{"title":"Summary"}`, stop: provider.StopReasonMaxTokens},
		{name: "tool use", text: `{"title":"Summary"}`, stop: provider.StopReasonToolUse},
		{name: "oversize", text: `{"title":"` + strings.Repeat("a", protocol.SessionTitleMaxBytes+1) + `"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			engine, backend := streamingTurn(t, &streamingTool{}, nil)
			backend.streams = []provider.Stream{&providerfixture.SliceStream{Events: []provider.StreamEvent{
				{Type: provider.EventUsage, Usage: &provider.Usage{InputTokens: 10, OutputTokens: 5}},
				{Type: provider.EventTextDelta, Text: test.text},
				{Type: provider.EventMessageStop, StopReason: test.stop},
			}}}
			engine.options.SharedRateLimit = NewSharedRateLimit(1)
			engine.options.Context.NarrativeTimeout = time.Second
			engine.syncSessionTitleState(provider.Usage{})
			result, err := engine.GenerateSessionTitle(t.Context(), "session:2", "Please improve how session titles are named")
			if (err == nil) != test.valid {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			if len(backend.requests) != 1 {
				t.Fatal("title sample retried")
			}
			request := backend.requests[0]
			if request.Purpose != model.PurposeSummary || len(request.Tools) != 0 ||
				request.NativeSearch || len(request.Messages) != 2 ||
				request.Messages[1].Blocks[0].Text != "Please improve how session titles are named" {
				t.Fatalf("request leaked main-agent context: %+v", request)
			}
			want := test.want
			if want == "" {
				want = "Improve session naming"
			}
			if test.valid && result.Title != want {
				t.Fatalf("title = %q", result.Title)
			}
			if len(engine.history) != 0 {
				t.Fatal("title sample entered assistant history")
			}
			usage, _ := engine.Usage()
			if usage.Total() != result.Usage.Total() || result.Usage.Total() == 0 {
				t.Fatal("title usage was not accounted")
			}
		})
	}
}

func TestSessionTitleHonorsExhaustedSessionBudget(t *testing.T) {
	engine, backend := streamingTurn(t, &streamingTool{}, nil)
	engine.options.SharedRateLimit = NewSharedRateLimit(1)
	engine.options.Context.NarrativeTimeout = time.Second
	engine.options.Budget.MaxTokens = 1
	engine.usage = provider.Usage{InputTokens: 1}
	engine.syncSessionTitleState(provider.Usage{})
	if _, err := engine.GenerateSessionTitle(t.Context(), "title", "Inspect parser"); !protocol.IsCode(err, protocol.CodeResourceExhausted) {
		t.Fatalf("exhausted budget error = %v", err)
	}
	if len(backend.requests) != 0 {
		t.Fatal("exhausted budget sent provider request")
	}
}

type interruptibleTitleProvider struct {
	primary provider.Provider
	started chan struct{}
}

func (p *interruptibleTitleProvider) Stream(ctx context.Context, request provider.ModelRequest) (provider.Stream, error) {
	if request.Purpose != model.PurposeSummary {
		return p.primary.Stream(ctx, request)
	}
	ctx, cancel := context.WithCancel(ctx)
	return &interruptibleTitleStream{ctx: ctx, cancel: cancel, started: p.started}, nil
}

type interruptibleTitleStream struct {
	ctx       context.Context
	cancel    context.CancelFunc
	started   chan struct{}
	sentUsage bool
}

func (s *interruptibleTitleStream) Recv() (provider.StreamEvent, error) {
	if !s.sentUsage {
		s.sentUsage = true
		return provider.StreamEvent{Type: provider.EventUsage, Usage: &provider.Usage{InputTokens: 10}}, nil
	}
	close(s.started)
	<-s.ctx.Done()
	return provider.StreamEvent{}, s.ctx.Err()
}

func (s *interruptibleTitleStream) Close() error { s.cancel(); return nil }

func TestSessionTitleDoesNotHoldTurnMutex(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	engine, backend := streamingTurn(t, &streamingTool{}, nil)
	started := make(chan struct{})
	engine.options.Provider = &interruptibleTitleProvider{primary: backend, started: started}
	engine.options.SharedRateLimit = NewSharedRateLimit(1)
	engine.options.Context.NarrativeTimeout = 5 * time.Second
	engine.syncSessionTitleState(provider.Usage{})
	titleCtx, cancelTitle := context.WithCancel(ctx)
	defer cancelTitle()
	done := make(chan error, 1)
	go func() {
		_, err := engine.GenerateSessionTitle(titleCtx, "title", "Inspect parser")
		done <- err
	}()
	select {
	case <-started:
	case <-ctx.Done():
		t.Fatal("title sample did not start")
	}
	preparing := make(chan struct{}, 1)
	turnDone := make(chan error, 1)
	go func() {
		_, err := engine.Run(ctx, "Continue", func(event Event) error {
			if event.State == Preparing {
				preparing <- struct{}{}
			}
			return nil
		})
		turnDone <- err
	}()
	select {
	case <-preparing:
	case <-ctx.Done():
		t.Fatal("title request blocked the turn mutex")
	}
	cancelTitle()
	if err := <-turnDone; err != nil {
		t.Fatal(err)
	}
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Fatalf("title cancellation: %v", err)
	}
	usage, _ := engine.Usage()
	if usage.Total() < 10 {
		t.Fatal("title usage was lost")
	}
}

func TestBackgroundSampleYieldsToForeground(t *testing.T) {
	limiter := NewSharedRateLimit(1)
	background, release, err := limiter.AcquireBackground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	acquired := make(chan struct{})
	go func() {
		defer close(acquired)
		foregroundRelease, err := limiter.Acquire(t.Context())
		if err == nil {
			foregroundRelease()
		}
	}()
	select {
	case <-background.Done():
	case <-time.After(time.Second):
		t.Fatal("foreground did not cancel background sample")
	}
	release()
	select {
	case <-acquired:
	case <-time.After(time.Second):
		t.Fatal("foreground did not receive the released slot")
	}
	foregroundRelease, err := limiter.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, _, err := limiter.AcquireBackground(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled background admission = %v", err)
	}
	foregroundRelease()
}

type concurrentTitleProvider struct{ primary provider.Provider }

func (p concurrentTitleProvider) Stream(ctx context.Context, request provider.ModelRequest) (provider.Stream, error) {
	if request.Purpose != model.PurposeSummary {
		return p.primary.Stream(ctx, request)
	}
	return &providerfixture.SliceStream{Events: []provider.StreamEvent{
		{Type: provider.EventTextDelta, Text: `{"title":"Inspect parser"}`},
		{Type: provider.EventUsage, Usage: &provider.Usage{InputTokens: 10, OutputTokens: 4}},
		{Type: provider.EventMessageStop, StopReason: provider.StopReasonEndTurn},
	}}, nil
}

func TestSessionTitleCompletesWhileTurnToolIsRunning(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	blocked := make(chan struct{})
	engine, backend := streamingTurn(t, &streamingTool{chunks: []string{"working"}, blocked: blocked}, nil)
	engine.options.Provider = concurrentTitleProvider{primary: backend}
	engine.options.SharedRateLimit = NewSharedRateLimit(1)
	engine.options.Context.NarrativeTimeout = time.Second
	working := make(chan struct{}, 1)
	done := make(chan error, 1)
	go func() {
		_, err := engine.Run(ctx, "Inspect parser", func(event Event) error {
			if event.ToolOutput != nil {
				select {
				case working <- struct{}{}:
				default:
				}
			}
			return nil
		})
		done <- err
	}()
	select {
	case <-working:
	case <-ctx.Done():
		t.Fatal("tool did not start")
	}
	result, err := engine.GenerateSessionTitle(ctx, "active-title", "Inspect parser")
	if err != nil || result.Title != "Inspect parser" {
		t.Fatalf("title waited for whole turn: %+v %v", result, err)
	}
	select {
	case err := <-done:
		t.Fatalf("turn ended before title generation: %v", err)
	default:
	}
	close(blocked)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
