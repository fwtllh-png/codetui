package turnkernel

import (
	"fmt"
	"strings"
)

// applyContinuationRecorded commits the cursor of a durable conversation
// snapshot. The sequence fence keeps the cursor monotonic per Turn: a
// restarted engine must extend the restored cursor instead of forking it.
func applyContinuationRecorded(
	transition *Transition,
	current State,
	command ContinuationRecorded,
) error {
	cursor := command.Cursor
	if cursor.Sequence == 0 ||
		strings.TrimSpace(cursor.Handle) == "" ||
		strings.TrimSpace(cursor.Digest) == "" {
		return illegal(current, command, "continuation cursor is incomplete")
	}
	expected := uint64(1)
	if current.Continuation != nil {
		expected = current.Continuation.Sequence + 1
	}
	if cursor.Sequence != expected {
		return illegal(current, command, fmt.Sprintf(
			"continuation sequence %d does not extend cursor %d",
			cursor.Sequence,
			expected-1,
		))
	}
	transition.State.Continuation = &cursor
	return nil
}
