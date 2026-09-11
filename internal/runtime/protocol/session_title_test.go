package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestSessionTitleContract(t *testing.T) {
	for _, title := range []string{
		strings.Repeat("a", SessionTitleMaxBytes), strings.Repeat("界", SessionTitleMaxBytes/3),
	} {
		if err := ValidateSessionTitle(title); err != nil {
			t.Fatal(err)
		}
	}
	for _, title := range []string{
		"", " \t", "a\nb", "a\rb", "a\x00b", "\xff",
		strings.Repeat("a", SessionTitleMaxBytes+1), strings.Repeat("界", SessionTitleMaxBytes/3+1),
	} {
		if ValidateSessionTitle(title) == nil {
			t.Fatalf("accepted invalid title %q", title)
		}
	}
	data := &SessionTitleUpdatedData{
		SessionID: "session", Title: "Inspect parser", TitleSource: SessionTitleAuto, TitleRevision: 3,
	}
	event, err := NewEvent(EventMeta{
		Sequence: 1, OperationID: "op", ThreadID: "thread", TurnID: "turn", ItemID: "item",
	}, data)
	if err != nil {
		t.Fatal(err)
	}
	encoded, err := json.Marshal(event)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Event
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Kind != EventSessionTitleUpdated || IsTerminalEvent(decoded.Kind) {
		t.Fatal("title event changed terminal state")
	}
	data.TitleSource = "invented"
	if data.validate() == nil {
		t.Fatal("accepted unknown title ownership")
	}
}
