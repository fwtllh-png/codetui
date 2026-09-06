package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	"github.com/fwtllh-png/QCode/internal/adapter/tool"
)

func TestSharedRateLimitAcquireWaitsForCooldown(t *testing.T) {
	shared := NewSharedRateLimit(1)
	shared.Record(40 * time.Millisecond)
	started := time.Now()
	release, err := shared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	release()
	if time.Since(started) < 30*time.Millisecond {
		t.Fatal("acquire skipped the recorded retry-after")
	}
}

func TestSharedRateLimitAcquireCancels(t *testing.T) {
	shared := NewSharedRateLimit(1)
	held, err := shared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() {
		_, err := shared.Acquire(ctx)
		done <- err
	}()
	cancel()
	if err := <-done; err == nil {
		t.Fatal("blocked acquire should fail when canceled")
	}
	held()
}

func TestSharedRateLimitBeginUserTurnKeepsCooldown(t *testing.T) {
	shared := NewSharedRateLimit(1)
	shared.Record(time.Hour)
	shared.BeginUserTurn()
	retries, waited := shared.Load()
	if retries != 0 || waited != 0 {
		t.Fatalf("user turn pot = %d %s", retries, waited)
	}
	if !shared.Hot() {
		t.Fatal("user turn must still honor remaining retry-after")
	}
	shared.ObserveSuccess()
	if shared.Hot() {
		t.Fatal("success should clear the storm")
	}
}

// TestSharedRateLimitSerializesProviderSamples locks the gate to one slot
// and keeps the historical single-flight guarantee for operators that
// declare max_concurrent = 1.
func TestSharedRateLimitSerializesProviderSamples(t *testing.T) {
	runtime := &blockingProvider{
		started: make(chan struct{}, 2),
		release: make(chan struct{}),
	}
	shared := NewSharedRateLimit(1)
	first := newEngine(t, runtime, tool.NewRegistry(nil, nil))
	second := newEngine(t, runtime, tool.NewRegistry(nil, nil))
	first.options.SharedRateLimit = shared
	second.options.SharedRateLimit = shared

	doneFirst := make(chan error, 1)
	go func() {
		_, err := first.Run(t.Context(), "first sample", nil)
		doneFirst <- err
	}()
	select {
	case <-runtime.started:
	case <-time.After(2 * time.Second):
		t.Fatal("first sample did not start")
	}

	doneSecond := make(chan error, 1)
	go func() {
		_, err := second.Run(t.Context(), "second sample", nil)
		doneSecond <- err
	}()
	select {
	case <-runtime.started:
		t.Fatal("second sample overlapped the first provider request")
	case <-time.After(50 * time.Millisecond):
	}
	if runtime.maxInFlight() != 1 {
		t.Fatalf("in-flight samples = %d, want 1", runtime.maxInFlight())
	}
	close(runtime.release)
	if err := <-doneFirst; err != nil {
		t.Fatalf("first sample: %v", err)
	}
	if err := <-doneSecond; err != nil {
		t.Fatalf("second sample: %v", err)
	}
	if runtime.maxInFlight() != 1 {
		t.Fatalf("serialized samples still overlapped: %d", runtime.maxInFlight())
	}
}

type blockingProvider struct {
	mu       sync.Mutex
	inFlight int
	max      int
	started  chan struct{}
	release  chan struct{}
}

func (p *blockingProvider) Stream(
	ctx context.Context,
	_ provider.ModelRequest,
) (provider.Stream, error) {
	p.mu.Lock()
	p.inFlight++
	if p.inFlight > p.max {
		p.max = p.inFlight
	}
	p.mu.Unlock()
	defer func() {
		p.mu.Lock()
		p.inFlight--
		p.mu.Unlock()
	}()
	select {
	case p.started <- struct{}{}:
	default:
	}
	select {
	case <-p.release:
		return textStream("ok"), nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

func (p *blockingProvider) maxInFlight() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.max
}

func TestSharedRateLimitAdmitsDeclaredConcurrency(t *testing.T) {
	runtime := &blockingProvider{
		started: make(chan struct{}, 4),
		release: make(chan struct{}),
	}
	shared := NewSharedRateLimit(2)
	engines := make([]*Engine, 3)
	for index := range engines {
		engines[index] = newEngine(t, runtime, tool.NewRegistry(nil, nil))
		engines[index].options.SharedRateLimit = shared
	}
	done := make([]chan error, len(engines))
	for index, engine := range engines {
		done[index] = make(chan error, 1)
		go func(index int, engine *Engine) {
			_, err := engine.Run(t.Context(), "sample", nil)
			done[index] <- err
		}(index, engine)
	}
	for range 2 {
		select {
		case <-runtime.started:
		case <-time.After(2 * time.Second):
			t.Fatal("declared concurrency did not admit two samples")
		}
	}
	// The third sample waits for a slot while the first two stay in flight.
	select {
	case <-runtime.started:
		t.Fatal("third sample exceeded the declared concurrency")
	case <-time.After(50 * time.Millisecond):
	}
	if runtime.maxInFlight() != 2 {
		t.Fatalf("in-flight samples = %d, want 2", runtime.maxInFlight())
	}
	close(runtime.release)
	for _, channel := range done {
		if err := <-channel; err != nil {
			t.Fatalf("sample: %v", err)
		}
	}
	if runtime.maxInFlight() != 2 {
		t.Fatalf("concurrent samples overlapped beyond the gate: %d", runtime.maxInFlight())
	}
}

func TestSharedRateLimitCooldownFreezesAllSlots(t *testing.T) {
	shared := NewSharedRateLimit(4)
	shared.Record(40 * time.Millisecond)
	started := time.Now()
	release, err := shared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	release()
	if time.Since(started) < 30*time.Millisecond {
		t.Fatal("cooldown did not gate an open slot")
	}
}

func TestSharedRateLimitZeroConcurrencyKeepsSingleFlight(t *testing.T) {
	shared := NewSharedRateLimit(0)
	held, err := shared.Acquire(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-shared.token:
		t.Fatal("zero concurrency admitted a second sample")
	default:
	}
	held()
}
