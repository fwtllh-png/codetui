package protocol

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestCommentaryContract(t *testing.T) {
	data := CommentaryCompletedData{
		MessageID: "turn/commentary/sample", SampleID: "sample",
		Text: "Checking callers.", CallIDs: []string{"call"},
	}
	event, err := NewEvent(EventMeta{
		Sequence: 1, OperationID: "op", ThreadID: "thread", TurnID: "turn", ItemID: "item",
	}, &data)
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
	if !reflect.DeepEqual(decoded.Data, &data) || IsTerminalEvent(event.Kind) {
		t.Fatalf("commentary round trip = %+v", decoded)
	}
	for _, mutate := range []func(*CommentaryCompletedData){
		func(d *CommentaryCompletedData) { d.MessageID = "" },
		func(d *CommentaryCompletedData) { d.SampleID = "" },
		func(d *CommentaryCompletedData) { d.Text = " \n" },
		func(d *CommentaryCompletedData) { d.CallIDs = nil },
		func(d *CommentaryCompletedData) { d.CallIDs = []string{""} },
		func(d *CommentaryCompletedData) { d.CallIDs = []string{"call", "call"} },
	} {
		invalid := data
		mutate(&invalid)
		if invalid.validate() == nil {
			t.Fatalf("accepted invalid commentary: %+v", invalid)
		}
	}
}
