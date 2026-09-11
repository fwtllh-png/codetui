package prompt

import (
	"path/filepath"
	"strings"
)

func StickyCacheKey(sessionID, workspace string) string {
	if sessionID != "" {
		return "session:" + sessionID
	}
	if workspace != "" {
		return "workspace:" + filepath.Base(workspace)
	}
	return "qcode-default"
}

// ToolInstructions builds the stable developer instruction for governed tools
// and appends an optional domain-specific contract.
func ToolInstructions(enabled bool, domain string) string {
	if !enabled {
		return ""
	}
	base := "Use only the supplied tools and honor their schemas and policy decisions. " +
		"Batch independent read-only calls in one response, use focused ranges, and do " +
		"not reread unchanged files when existing evidence answers the question. " +
		"A completed tool call is a normal sample boundary, not a truncated response. " +
		"Only report or reason about output truncation when the current request contains " +
		"the structured [continue_after_incomplete] feedback. " +
		"A tool-enabled Turn ends when you stop calling tools and write the " +
		"user-facing answer as ordinary assistant text. Call request_user_input when " +
		"progress truly requires a user answer and wait in the same Turn. " +
		"turn_complete is optional: use status=complete with output_mode=exact only " +
		"to replace the captured answer, or status=incomplete with concrete " +
		"pending_actions when work remains. If the user only asked for a plan they " +
		"will execute themselves, call submit_plan with purpose=deliverable and leave " +
		"future steps pending. A deliverable does not replace the current execution " +
		"plan or authorize its steps. Use update_plan only for work required in this " +
		"Turn, and submit_plan with purpose=execution when the user requested " +
		"implementation. Never reclassify unfinished execution work as a deliverable. " +
		"Do not rewrite an unchanged plan. Open execution plan steps do not block " +
		"stopping."
	if domain = strings.TrimSpace(domain); domain != "" {
		return base + " " + domain
	}
	return base
}
