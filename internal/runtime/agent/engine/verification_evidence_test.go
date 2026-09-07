package engine

import (
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	"github.com/fwtllh-png/QCode/internal/adapter/tool"
	"github.com/fwtllh-png/QCode/internal/observability/verify"
	agentcontext "github.com/fwtllh-png/QCode/internal/runtime/agent/context"
	"github.com/fwtllh-png/QCode/internal/runtime/agent/turnkernel"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

type unavailableVerifier struct{}

func (e *Engine) commandVerificationReceipt(paths []string, revision uint64) (verify.Receipt, []string) {
	canonical := make([]string, len(paths))
	for index, path := range paths {
		canonical[index] = path
		if relative, ok := agentcontext.WorkspaceRelative(e.options.Workspace, path); ok {
			canonical[index] = relative
		}
	}
	return verify.CommandEvidenceReceipt(canonical, revision, e.verificationEvidence())
}

func (unavailableVerifier) Verify(
	_ context.Context, request verify.Request,
) (verify.Receipt, error) {
	if len(request.Evidence) != 0 {
		receipt, _ := verify.CommandEvidenceReceipt(request.Paths, request.MutationRevision, request.Evidence)
		return receipt, nil
	}
	return verify.Receipt{
		Scope: request.Scope, Status: verify.StatusUnavailable,
		UncoveredPaths: append([]string(nil), request.Paths...),
		Message:        "diagnostics unavailable",
	}, nil
}

func TestCommandEvidenceCoversCurrentMutationRevision(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	result := commandEvidenceResult(
		verify.StatusPassed, []string{"a.go", "b.go"},
	)
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-1", Name: "exec_command",
	}, &result, false, 1)

	receipt, uncovered := engine.commandVerificationReceipt([]string{"a.go", "b.go"}, 1)
	if receipt.Status != verify.StatusPassed || len(uncovered) != 0 {
		t.Fatalf("receipt = %+v, uncovered = %v", receipt, uncovered)
	}
	evidence := result.Outcome.Facts.Verification
	if evidence == nil || evidence.CallID != "verify-1" || evidence.MutationRevision != 1 {
		t.Fatalf("bound evidence = %#v", result.Outcome)
	}
}

func TestCommandEvidenceCoversAbsoluteRecoveryDraftPaths(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	engine.options.Workspace = t.TempDir()
	result := commandEvidenceResult(verify.StatusPassed, []string{"a.go"})
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-1", Name: "exec_command",
	}, &result, false, 1)

	receipt, uncovered := engine.commandVerificationReceipt(
		[]string{filepath.Join(engine.options.Workspace, "a.go")},
		1,
	)
	if receipt.Status != verify.StatusPassed || len(uncovered) != 0 {
		t.Fatalf("receipt = %+v, uncovered = %v", receipt, uncovered)
	}
}

func TestCommandEvidenceRejectsPartialAndStaleCoverage(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	result := commandEvidenceResult(verify.StatusPassed, []string{"a.go"})
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-1", Name: "exec_command",
	}, &result, false, 1)

	receipt, uncovered := engine.commandVerificationReceipt([]string{"a.go", "b.go"}, 1)
	if receipt.Status != verify.StatusUnavailable ||
		len(uncovered) != 1 || uncovered[0] != "b.go" {
		t.Fatalf("receipt = %+v, uncovered = %v", receipt, uncovered)
	}
	receipt, uncovered = engine.commandVerificationReceipt([]string{"a.go"}, 2)
	if receipt.Status != verify.StatusUnavailable ||
		len(uncovered) != 1 || uncovered[0] != "a.go" {
		t.Fatalf("stale receipt = %+v, uncovered = %v", receipt, uncovered)
	}
}

func TestCommandEvidenceRetainsFailureWithoutGrantingCoverage(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	result := commandEvidenceResult(verify.StatusFailed, []string{"a.go"})
	result.IsError = true
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-failed", Name: "exec_command",
	}, &result, false, 1)

	if accepted, _ := result.Metadata["verification_evidence_accepted"].(bool); !accepted {
		t.Fatalf("failed structured evidence was discarded: %#v", result.Metadata)
	}
	candidate := engine.completionCandidate(
		provider.ToolCall{ID: "complete", Name: "turn_complete"},
		tool.Result{}, false, 1, 1,
	)
	if len(candidate.VerificationCalls) != 0 {
		t.Fatalf("failed verification calls bound to completion: %+v", candidate.VerificationCalls)
	}
	receipt, uncovered := engine.commandVerificationReceipt([]string{"a.go"}, 1)
	if receipt.Status != verify.StatusFailed || receipt.Errors != 1 ||
		len(receipt.Checks) != 1 ||
		receipt.Checks[0].Status != verify.StatusFailed ||
		len(uncovered) != 1 || uncovered[0] != "a.go" {
		t.Fatalf("receipt = %+v, uncovered = %v", receipt, uncovered)
	}
}

func TestLaterPassingCommandRetrySupersedesSameFailedCommand(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	failed := commandEvidenceResult(verify.StatusFailed, []string{"a.go"})
	failed.IsError = true
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-failed", Name: "exec_command",
	}, &failed, false, 1)
	passed := commandEvidenceResult(verify.StatusPassed, []string{"a.go"})
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-passed", Name: "exec_command",
	}, &passed, false, 1)

	receipt, uncovered := engine.commandVerificationReceipt([]string{"a.go"}, 1)
	if receipt.Status != verify.StatusPassed || len(uncovered) != 0 ||
		len(receipt.Checks) != 1 ||
		receipt.Checks[0].Status != verify.StatusPassed {
		t.Fatalf("receipt = %+v, uncovered = %v", receipt, uncovered)
	}
}

func TestCorrectedQualityCommandSupersedesFailedCoverage(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	failed := commandEvidenceResult(verify.StatusFailed, []string{"a.go"})
	failed.IsError = true
	failed.Outcome.Facts.Verification.CommandDigest = "sha256:bad-command"
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-failed", Name: "exec_command",
	}, &failed, false, 1)
	passed := commandEvidenceResult(verify.StatusPassed, []string{"a.go"})
	passed.Outcome.Facts.Verification.CommandDigest = "sha256:fixed-command"
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-passed", Name: "exec_command",
	}, &passed, false, 1)

	receipt, uncovered := engine.commandVerificationReceipt([]string{"a.go"}, 1)
	if receipt.Status != verify.StatusPassed || len(uncovered) != 0 ||
		len(receipt.Checks) != 1 ||
		receipt.Checks[0].Command != "sha256:fixed-command" {
		t.Fatalf("receipt = %+v, uncovered = %v", receipt, uncovered)
	}
}

func TestPassingTestCannotSupersedeFailedBuild(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	failedBuild := commandEvidenceResult(verify.StatusFailed, []string{"a.go"})
	failedBuild.IsError = true
	failedBuild.Outcome.Facts.Verification.Kind = "build"
	failedBuild.Outcome.Facts.Verification.CommandDigest = "sha256:build"
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "build-failed", Name: "exec_command",
	}, &failedBuild, false, 1)

	passedVerify := commandEvidenceResult(verify.StatusPassed, []string{"a.go"})
	passedVerify.Outcome.Facts.Verification.Kind = "verify"
	passedVerify.Outcome.Facts.Verification.CommandDigest = "sha256:verify"
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-passed", Name: "exec_command",
	}, &passedVerify, false, 1)

	receipt, uncovered := engine.commandVerificationReceipt([]string{"a.go"}, 1)
	if receipt.Status != verify.StatusFailed || receipt.Errors != 1 ||
		len(uncovered) != 0 {
		t.Fatalf("generic verify cleared process build failure: %+v, %v", receipt, uncovered)
	}

	passedBuild := commandEvidenceResult(verify.StatusPassed, []string{"a.go"})
	passedBuild.Outcome.Facts.Verification.Kind = "build"
	passedBuild.Outcome.Facts.Verification.CommandDigest = "sha256:build-retry"
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "build-passed", Name: "exec_command",
	}, &passedBuild, false, 1)

	receipt, uncovered = engine.commandVerificationReceipt([]string{"a.go"}, 1)
	if receipt.Status != verify.StatusPassed || len(uncovered) != 0 {
		t.Fatalf("passing process build did not clear prior failure: %+v, %v", receipt, uncovered)
	}
}

func TestRunningCommandDoesNotCoverChangedPaths(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	passedBuild := commandEvidenceResult(verify.StatusRunning, []string{"a.go"})
	passedBuild.Outcome.Facts.Verification.Kind = "build"
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "build-passed", Name: "exec_command",
	}, &passedBuild, false, 1)

	receipt, uncovered := engine.commandVerificationReceipt([]string{"a.go"}, 1)
	if receipt.Status != verify.StatusUnavailable ||
		len(uncovered) != 1 || uncovered[0] != "a.go" {
		t.Fatalf("process build covered verification paths: %+v, %v", receipt, uncovered)
	}
}

func TestCommandEvidenceRejectsSameBatchMutationAndGenericShell(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	result := commandEvidenceResult(verify.StatusPassed, []string{"a.go"})
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-1", Name: "exec_command",
	}, &result, true, 1)
	if accepted, _ := result.Metadata["verification_evidence_accepted"].(bool); accepted {
		t.Fatalf("same-batch evidence was accepted: %#v", result.Metadata)
	}
	shellResult := commandEvidenceResult(verify.StatusPassed, []string{"a.go"})
	shellResult.Execution.VerificationEvidenceAuthorized = false
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "shell-1", Name: "exec_command",
	}, &shellResult, false, 1)
	receipt, uncovered := engine.commandVerificationReceipt([]string{"a.go"}, 1)
	if receipt.Status != verify.StatusUnavailable || len(uncovered) != 1 {
		t.Fatalf("generic shell produced evidence: %+v, %v", receipt, uncovered)
	}
}

func TestVerifyGateAcceptsFullCurrentCommandCoverage(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	scope := attachTestScope(t, engine)
	engine.options.Verify = VerifyOptions{
		Mode: VerifyModeSoft, Scope: verify.ScopeDiagnostics,
		Runner: unavailableVerifier{}, MaxRepairSteps: 1,
	}
	scope.state.diff.Record(turnkernel.TurnDiffEntry{Path: "a.go", Kind: "modified"})
	result := commandEvidenceResult(verify.StatusPassed, []string{"a.go"})
	engine.bindVerificationEvidence(provider.ToolCall{
		ID: "verify-1", Name: "exec_command",
	}, &result, false, 1)
	kernel := newEngineTurnKernel(
		protocol.TurnIntentAnswer, "act", nil, 0, nil, nil,
	)
	seedKernelMutation(t, kernel)
	gate := verifyGate{
		engine: engine,
		kernel: kernel,
	}

	outcome, err := gate.evaluate(t.Context(), func(State, Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if outcome.action != verifyActionPassed || outcome.receipt == nil ||
		outcome.receipt.Status != verify.StatusPassed ||
		outcome.receipt.Scope != verify.ScopeCommands {
		t.Fatalf("outcome = %+v", outcome)
	}
}

func TestVerifyGateRequestsStructuredRepairForMissingCoverage(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	scope := attachTestScope(t, engine)
	engine.options.Verify = VerifyOptions{
		Mode: VerifyModeHard, Scope: verify.ScopeDiagnostics,
		Runner: unavailableVerifier{}, MaxRepairSteps: 1,
	}
	scope.state.diff.Record(turnkernel.TurnDiffEntry{Path: "a.go", Kind: "modified"})
	kernel := newEngineTurnKernel(
		protocol.TurnIntentAnswer, "act", nil, 0, nil, nil,
	)
	seedKernelMutation(t, kernel)
	gate := verifyGate{
		engine: engine, kernel: kernel,
	}

	outcome, err := gate.evaluate(t.Context(), func(State, Event) error { return nil })
	if err != nil {
		t.Fatal(err)
	}
	if outcome.action != verifyActionRepair || outcome.receipt == nil ||
		len(outcome.receipt.UncoveredPaths) != 1 || outcome.receipt.UncoveredPaths[0] != "a.go" {
		t.Fatalf("outcome = %+v", outcome)
	}
	feedback := verifyFeedback(outcome.receipt, 1)
	if !strings.Contains(feedback.Text(), "required_action=exec_command") {
		t.Fatalf("feedback = %q", feedback.Text())
	}
}

func TestFailedCommandFeedbackRequiresPassingStructuredRetry(t *testing.T) {
	receipt := &VerificationReceipt{
		Receipt: verify.Receipt{
			UncoveredPaths: []string{"a.go"},
			Scope:          verify.ScopeCommands, Status: verify.StatusFailed,
			Message: "declared verification command failed",
			Checks: []verify.Check{{
				Name: "test", Command: "sha256:test",
				Status: verify.StatusFailed, ExitCode: 1,
			}},
		},
	}
	feedback := verifyFeedback(receipt, 1).Text()
	for _, expected := range []string{
		"required_action=exec_command",
		`uncovered_paths=["a.go"]`,
		"network_targets",
		"write_stdin",
	} {
		if !strings.Contains(feedback, expected) {
			t.Fatalf("feedback %q does not contain %q", feedback, expected)
		}
	}
}

func seedKernelMutation(t *testing.T, kernel *turnkernel.RuntimeKernel) {
	t.Helper()
	call := provider.ToolCall{ID: "write-1", Name: "file_write"}
	if err := kernel.StartTools([]provider.ToolCall{call}); err != nil {
		t.Fatal(err)
	}
	if err := kernel.StartTool(call.ID); err != nil {
		t.Fatal(err)
	}
	if err := kernel.CloseTool(
		call,
		tool.Result{},
		[]tool.WorkspaceChange{{
			Path: "a.go",
			Kind: tool.WorkspaceModified,
		}},
	); err != nil {
		t.Fatal(err)
	}
}

func TestStrictVerifyGateDoesNotReportFailedVerification(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	kernel := newEngineTurnKernel(
		protocol.TurnIntentAnswer, "act", nil, 0, nil, nil,
	)
	seedKernelMutation(t, kernel)
	gate := verifyGate{
		engine: engine, kernel: kernel,
	}
	if err := kernel.BeginVerification(); err != nil {
		t.Fatal(err)
	}
	command := gate.verificationCommand(verify.Receipt{Status: verify.StatusFailed, Message: "broken"})
	action, err := kernel.FinishVerification(command)
	if err != nil {
		t.Fatal(err)
	}
	if action != turnkernel.VerificationActionRepair {
		t.Fatalf("first strict failed verification action = %q", action)
	}
	if err := kernel.BeginVerification(); err != nil {
		t.Fatal(err)
	}
	action, err = kernel.FinishVerification(command)
	if err != nil {
		t.Fatal(err)
	}
	if action != turnkernel.VerificationActionBlocked {
		t.Fatalf("strict failed verification action = %q", action)
	}
}

func commandEvidenceResult(status string, paths []string) tool.Result {
	return tool.Result{
		Execution: &tool.ExecutionReceipt{
			VerificationEvidenceAuthorized: true,
		},
		Outcome: &tool.Outcome{Facts: &tool.OutcomeFacts{
			Verification: &verify.Evidence{
				SchemaVersion: 1, Kind: "verify", Status: status,
				CoveredPaths: paths, CommandDigest: "sha256:test",
			},
		}},
	}
}
