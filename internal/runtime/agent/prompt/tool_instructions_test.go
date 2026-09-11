package prompt

import (
	"strings"
	"testing"
)

func TestToolInstructionsRequireOneStepStructuredTerminalState(
	t *testing.T,
) {
	instructions := ToolInstructions(true, "")
	for _, required := range []string{
		"request_user_input",
		"stop calling tools and write the user-facing answer",
		"ordinary assistant text",
		"turn_complete is optional",
		"status=incomplete",
		"purpose=deliverable",
		"leave future steps pending",
		"does not replace the current execution plan",
		"purpose=execution",
		"Never reclassify",
		"Open execution plan steps do not block",
		"Batch independent read-only calls",
		"do not reread unchanged files",
		"normal sample boundary, not a truncated response",
		"structured [continue_after_incomplete] feedback",
	} {
		if !strings.Contains(instructions, required) {
			t.Fatalf("tool instructions missing %q: %q", required, instructions)
		}
	}
	if strings.Contains(instructions, "Ordinary assistant text is provisional") {
		t.Fatalf("tool instructions still force a structured terminal: %q", instructions)
	}
}
