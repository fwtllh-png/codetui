package turnkernel

import (
	"reflect"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func TestCommentaryClassificationPreservesFinalOutputContract(t *testing.T) {
	for _, test := range []struct {
		name       string
		text       string
		calls      []ToolCallState
		converging bool
		failed     bool
		commentary bool
		final      bool
	}{
		{name: "tool preface", text: "I found the handler. Checking its callers.",
			calls: []ToolCallState{{ID: "read", Name: "file_read"}}, commentary: true},
		{name: "no text", calls: []ToolCallState{{ID: "read", Name: "file_read"}}},
		{name: "whitespace", text: " \n", calls: []ToolCallState{{ID: "read", Name: "file_read"}}},
		{name: "plain answer", text: "Answer.", final: true},
		{name: "declaration", text: "Verified.",
			calls: []ToolCallState{{ID: "done", Name: "turn_complete"}}, final: true},
		{name: "converging", text: "Retained answer.", converging: true,
			calls: []ToolCallState{{ID: "read", Name: "file_read"}}, final: true},
		{name: "failed sample", text: "Unconfirmed text.", failed: true,
			calls: []ToolCallState{{ID: "read", Name: "file_read"}}},
		{name: "multi tool", text: "Checking both callers.",
			calls:      []ToolCallState{{ID: "a", Name: "file_read"}, {ID: "b", Name: "file_read"}},
			commentary: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := startSampling(t, protocol.TurnIntentAnswer)
			if test.converging {
				state = apply(t, state, ConvergenceRequested{
					Cause: ConvergenceStepLimit, Used: 1, Limit: 1,
				}).State
			}
			state = apply(t, state, ModelSampleRequested{SampleID: "sample"}).State
			effect := pendingEffectID(state, EffectSampleProvider, "sample")
			state = apply(t, state, EffectStarted{EffectID: effect, Attempt: 1}).State
			command := ModelSampleResultReceived{
				EffectID: effect, SampleID: "sample", Text: test.text, Calls: test.calls,
			}
			if test.failed {
				command.Error = "stream failed"
				command.Failure = &provider.Failure{Code: provider.FailureStreamClosed, Message: command.Error}
			}
			result := apply(t, state, command).State
			if (len(result.Commentary) != 0) != test.commentary ||
				(len(result.ProvisionalOutput) != 0) != test.final ||
				len(result.FinalOutput) != 0 || result.OutputEligibility {
				t.Fatalf("commentary=%+v provisional=%v final=%v eligible=%v",
					result.Commentary, result.ProvisionalOutput, result.FinalOutput, result.OutputEligibility)
			}
			if test.commentary {
				message := result.Commentary[0]
				if message.SampleID != "sample" || message.Text != test.text ||
					len(message.CallIDs) != len(test.calls) {
					t.Fatalf("message = %+v", message)
				}
				if FormatProgressSignature(result, 0, false) != FormatProgressSignature(state, 0, false) {
					t.Fatal("commentary counted as work progress")
				}
				cloned := cloneState(result)
				cloned.Commentary[0].CallIDs[0] = "changed"
				if result.Commentary[0].CallIDs[0] != test.calls[0].ID {
					t.Fatal("snapshot shares mutable commentary call ids")
				}
			}
		})
	}
}

func TestCommentaryRecoversFromAcceptedSampleBeforePublication(t *testing.T) {
	store := NewMemoryTerminalEnvelopeStore(nil, nil)
	runtime, err := NewStoreCoordinatorRuntime(store)
	if err != nil {
		t.Fatal(err)
	}
	open := func() *RuntimeKernel {
		kernel, err := NewRuntimeKernel(
			KernelIdentity{TurnID: "turn-commentary", ProfileRevision: 1},
			protocol.TurnIntentAnswer, "act", nil, false, nil,
			nil, nil, nil, nil, DefaultPolicy(), runtime,
		)
		if err != nil {
			t.Fatal(err)
		}
		return kernel
	}
	kernel := open()
	if err := kernel.BeginModelSample(t.Context(), "sample"); err != nil {
		t.Fatal(err)
	}
	if err := kernel.FinishModelSample("sample", "Checking callers.",
		[]provider.ToolCall{{ID: "read", Name: "file_read", Arguments: `{}`}},
		provider.Usage{}, 0, false, false, nil, nil); err != nil {
		t.Fatal(err)
	}
	before := kernel.CommentaryMessages()
	if len(before) != 1 || kernel.SampleCommentary("other") != nil {
		t.Fatalf("messages = %+v", before)
	}
	if err := runtime.Release(t.Context(), "turn-commentary"); err != nil {
		t.Fatal(err)
	}
	kernel = open()
	defer runtime.Release(t.Context(), "turn-commentary")
	if !reflect.DeepEqual(before, kernel.CommentaryMessages()) ||
		len(kernel.PendingToolCalls()) != 1 || kernel.HasProvisionalOutput() {
		t.Fatal("recovery changed commentary, pending tools, or final candidate")
	}
	if kernel.SampleCommentary("sample").MessageID != before[0].MessageID {
		t.Fatal("message identity changed")
	}
	if err := kernel.RequestCancel("user_interrupted"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(before, kernel.CommentaryMessages()) || kernel.HasProvisionalOutput() {
		t.Fatal("cancellation changed confirmed commentary or released a final candidate")
	}
}
