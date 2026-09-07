package turnkernel

import (
	"errors"
	"testing"

	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func TestContinuationRecordedCommitsMonotonicCursor(t *testing.T) {
	state := startSampling(t, protocol.TurnIntentAnswer)
	first := apply(t, state, ContinuationRecorded{Cursor: ContinuationCursor{
		Sequence: 1, Handle: "handle-1", Digest: "digest-1",
	}})
	if first.State.Continuation == nil ||
		first.State.Continuation.Sequence != 1 ||
		first.State.Continuation.Handle != "handle-1" {
		t.Fatalf("first continuation cursor = %+v", first.State.Continuation)
	}

	extended := apply(t, first.State, ContinuationRecorded{Cursor: ContinuationCursor{
		Sequence: 2, Handle: "handle-2", Digest: "digest-2",
	}})
	if extended.State.Continuation.Sequence != 2 ||
		extended.State.Continuation.Handle != "handle-2" {
		t.Fatalf("extended continuation cursor = %+v", extended.State.Continuation)
	}
}

func TestContinuationRecordedRejectsForkedCursor(t *testing.T) {
	state := startSampling(t, protocol.TurnIntentAnswer)
	state = apply(t, state, ContinuationRecorded{Cursor: ContinuationCursor{
		Sequence: 1, Handle: "handle-1", Digest: "digest-1",
	}}).State
	_, err := (Reducer{}).Apply(state, ContinuationRecorded{Cursor: ContinuationCursor{
		Sequence: 3, Handle: "handle-3", Digest: "digest-3",
	}})
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("forked cursor error = %v", err)
	}
	_, err = (Reducer{}).Apply(state, ContinuationRecorded{Cursor: ContinuationCursor{
		Sequence: 2, Handle: "", Digest: "digest-2",
	}})
	if !errors.Is(err, ErrIllegalTransition) {
		t.Fatalf("incomplete cursor error = %v", err)
	}
}
