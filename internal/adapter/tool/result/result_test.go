package result

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	"github.com/fwtllh-png/QCode/internal/adapter/tool"
)

func TestSuccessFailureAndUnavailable(t *testing.T) {
	success, err := Success(map[string]any{"ok": true}, map[string]any{"count": 1})
	if err != nil {
		t.Fatal(err)
	}
	if success.Content != `{"ok":true}` || success.IsError || success.Metadata["count"] != 1 {
		t.Fatalf("success = %+v", success)
	}
	failure := Fail(Failure{
		Category: "fixture", Message: "failed", Retryable: true,
		Metadata: map[string]any{"detail": "x"},
	})
	if !failure.IsError || failure.Metadata["error_category"] != "fixture" ||
		failure.Metadata["retryable"] != true || failure.Metadata["detail"] != "x" {
		t.Fatalf("failure = %+v", failure)
	}
	unavailable := Unavailable("offline")
	if !unavailable.IsError || unavailable.Metadata["error_category"] != "unavailable" {
		t.Fatalf("unavailable = %+v", unavailable)
	}
}

func TestValidateRejectsNonJSONMetadata(t *testing.T) {
	value := Text("arbitrary text", map[string]any{"bad": make(chan struct{})})
	if err := Validate(value); err == nil {
		t.Fatal("non-JSON metadata succeeded")
	}
}

func TestPruneSurfacesProtectsLatestUnconsumedBatch(t *testing.T) {
	store := tool.NewResultStore(32 << 10)
	registry := tool.NewRegistry(nil, store)
	oldContent := strings.Repeat("old ", 1200)
	latestContent := strings.Repeat("latest ", 1200)
	history := []provider.Message{
		toolCallMessage("old", "file_read"),
		toolResultMessage(t, "old", oldContent),
		toolCallMessage("latest", "file_read"),
		toolResultMessage(t, "latest", latestContent),
	}
	stats, _, err := PruneSurfaces(
		&history, registry, 256, true,
		func([]provider.Message) (PruneWindow, error) {
			return PruneWindow{}, nil
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	if stats.Results != 1 {
		t.Fatalf("stats = %+v", stats)
	}
	var latest tool.Result
	if err := json.Unmarshal(
		[]byte(history[3].Blocks[0].ToolResult.Content),
		&latest,
	); err != nil {
		t.Fatal(err)
	}
	if latest.Content != latestContent || latest.Truncated {
		t.Fatalf("latest result was pruned before consumption: %+v", latest)
	}
}

func toolCallMessage(id, name string) provider.Message {
	call := provider.ToolCall{ID: id, Name: name, Arguments: `{}`}
	return provider.Message{
		Role: provider.RoleAssistant,
		Blocks: []provider.ContentBlock{{
			Type: provider.ContentToolCall, ToolCall: &call,
		}},
	}
}

func toolResultMessage(t *testing.T, id, content string) provider.Message {
	t.Helper()
	encoded, err := json.Marshal(tool.Result{Content: content})
	if err != nil {
		t.Fatal(err)
	}
	return provider.Message{
		Role: provider.RoleTool,
		Blocks: []provider.ContentBlock{{
			Type: provider.ContentToolResult,
			ToolResult: &provider.ToolResult{
				CallID: id, Content: string(encoded),
			},
		}},
	}
}

func TestCollapseSurfacesBeforeRewritesOlderResultsOnly(t *testing.T) {
	store := tool.NewResultStore(32 << 10)
	registry := tool.NewRegistry(nil, store)
	oldContent := strings.Repeat("old-result ", 200)
	newContent := strings.Repeat("new-result ", 200)
	history := []provider.Message{
		toolCallMessage("call-old", "file_read"),
		toolResultMessage(t, "call-old", oldContent),
		toolCallMessage("call-new", "file_read"),
		toolResultMessage(t, "call-new", newContent),
	}
	stats := CollapseSurfacesBefore(&history, registry, 2)
	if stats.Results != 1 {
		t.Fatalf("collapsed = %d, want 1", stats.Results)
	}
	var oldProjected, newProjected tool.Result
	if err := json.Unmarshal([]byte(history[1].Blocks[0].ToolResult.Content), &oldProjected); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(history[3].Blocks[0].ToolResult.Content), &newProjected); err != nil {
		t.Fatal(err)
	}
	if oldProjected.Handle == "" || newProjected.Content != newContent {
		t.Fatalf("old=%+v new=%+v", oldProjected, newProjected)
	}
	full, found := store.Get(oldProjected.Handle)
	if !found || full != oldContent {
		t.Fatalf("stored old result found=%t", found)
	}
}

func editHintError(category string, matchCount int, excerpt string) error {
	hint := tool.RecoveryHint{
		ErrorCategory:  category,
		RequiredAction: "file_read",
		Path:           "pkg/node.cpp",
		RetryOriginal:  false,
		FailedChange:   1,
		MatchCount:     matchCount,
		StartLine:      74,
		EndLine:        80,
		CurrentExcerpt: excerpt,
	}
	return tool.WithRecoveryHint(
		errors.New("old text matched "+strconv.Itoa(matchCount)+
			" times, want exactly once"),
		hint,
	)
}

func TestRecoverableFailureAmbiguousMatchAdvisesWideningOld(t *testing.T) {
	content, recoverable := RecoverableFailure(editHintError(
		"edit_precondition_miss", 2, "current text",
	))
	if !recoverable {
		t.Fatal("ambiguous edit failure is not recoverable")
	}
	if !strings.Contains(content, "match_count=2") ||
		!strings.Contains(content,
			"extend old with more surrounding lines") {
		t.Fatalf("content = %q", content)
	}
}

func TestRecoverableFailureWithoutExcerptAdvisesExactCopy(t *testing.T) {
	content, recoverable := RecoverableFailure(editHintError(
		"edit_precondition_miss", 0, "",
	))
	if !recoverable {
		t.Fatal("miss edit failure is not recoverable")
	}
	if !strings.Contains(content, "no similar location found") ||
		!strings.Contains(content, "copy old exactly") ||
		strings.Contains(content, "current_excerpt_lines") {
		t.Fatalf("content = %q", content)
	}
}

func TestRecoverableFailureWithExcerptUnchanged(t *testing.T) {
	content, recoverable := RecoverableFailure(editHintError(
		"edit_precondition_miss", 0, "current text",
	))
	if !recoverable {
		t.Fatal("excerpt edit failure is not recoverable")
	}
	if !strings.Contains(content, "current_excerpt_lines=74-80:\ncurrent text") {
		t.Fatalf("content = %q", content)
	}
	if strings.Contains(content, "no similar location") ||
		strings.Contains(content, "extend old") {
		t.Fatalf("unexpected extra guidance: %q", content)
	}
}
