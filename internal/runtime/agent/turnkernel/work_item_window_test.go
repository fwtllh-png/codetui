package turnkernel

import (
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/tool"
)

func TestObserveWorkItemResultRecordsStructuredReadWindow(t *testing.T) {
	observation := ObserveWorkItemResult(
		ToolCallState{ID: "call-1", Name: "file_read", Arguments: `{"path":"parser.go","start_line":40,"max_lines":20}`},
		tool.Result{
			Content: "window",
			Metadata: map[string]any{
				"returned_lines": 20,
				"has_more":       true,
			},
			Outcome: &tool.Outcome{Facts: &tool.OutcomeFacts{
				WorkspaceRead: &tool.WorkspaceReadFact{
					Path: "parser.go", Digest: "sha256:abc",
				},
				ResultHandle: "result-handle-1",
			}},
		},
	)
	if observation.ReadPath != "parser.go" ||
		observation.ReadStart != 40 ||
		observation.ReadEnd != 60 ||
		observation.ContentDigest != "sha256:abc" ||
		observation.ResultHandle != "result-handle-1" ||
		observation.CallID != "call-1" {
		t.Fatalf("structured observation = %+v", observation)
	}

	state := NewState("answer", "act", 1)
	state.Phase = PhaseExecutingTools
	applyWorkItemObservation(&state, ToolCallState{ID: "call-1"}, nil, observation, false)
	read, ok := state.WorkItem.KnownReads["parser.go"]
	if !ok ||
		read.StartLine != 40 ||
		read.EndLine != 60 ||
		read.ContentDigest != "sha256:abc" ||
		read.ResultHandle != "result-handle-1" ||
		read.CallID != "call-1" ||
		read.Window != "40" {
		t.Fatalf("recorded read = %+v", read)
	}
}

func TestObserveWorkItemResultBoundedWindowReachingEOF(t *testing.T) {
	observation := ObserveWorkItemResult(
		ToolCallState{ID: "call-2", Name: "file_read", Arguments: `{"path":"parser.go","start_line":40}`},
		tool.Result{
			Content: "tail",
			Metadata: map[string]any{
				"returned_lines": 7,
				"has_more":       false,
			},
		},
	)
	if observation.ReadStart != 40 || observation.ReadEnd != 0 {
		t.Fatalf("eof-reaching observation = %+v", observation)
	}
}

func TestReadWindowCovers(t *testing.T) {
	cases := []struct {
		name      string
		read      WorkItemRead
		startLine int
		covered   bool
	}{
		{"full covers full", WorkItemRead{Window: "full"}, 0, true},
		{"full covers window", WorkItemRead{Window: "full"}, 30, true},
		{
			"legacy start window covers later start",
			WorkItemRead{Window: "40"}, 60, true,
		},
		{
			"legacy start window misses earlier start",
			WorkItemRead{Window: "40"}, 10, false,
		},
		{
			"legacy start window misses full request",
			WorkItemRead{Window: "40"}, 0, false,
		},
		{
			"bounded window covers inner start",
			WorkItemRead{Window: "40", StartLine: 40, EndLine: 60}, 50, true,
		},
		{
			"bounded window misses start at end",
			WorkItemRead{Window: "40", StartLine: 40, EndLine: 60}, 60, false,
		},
		{
			"structured window without legacy string",
			WorkItemRead{StartLine: 5}, 9, true,
		},
		// StartLine 0 means the read started at the top of the file, so an
		// unannotated record still claims whole-file coverage; replay safety
		// comes from the digest gate, not from the window alone.
		{
			"unannotated record claims full coverage",
			WorkItemRead{}, 1, true,
		},
	}
	for _, testCase := range cases {
		if got := ReadWindowCovers(testCase.read, testCase.startLine); got != testCase.covered {
			t.Fatalf(
				"%s: ReadWindowCovers(%+v, %d) = %t, want %t",
				testCase.name,
				testCase.read,
				testCase.startLine,
				got,
				testCase.covered,
			)
		}
	}
}
