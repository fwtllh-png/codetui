package turnkernel

import "testing"

func TestFinalizationDeclarationRepairUsesExistingBoundedBudget(t *testing.T) {
	state := State{
		Phase:         PhaseSampling,
		Convergence:   &ConvergenceState{Cause: ConvergenceNoProgress, FinalizationAttempted: true},
		Completion:    &CompletionDecision{Reason: "invalid_declaration"},
		RepairBudgets: make(map[RepairKind]RepairBudget),
	}
	state.Policy.DeclarationRepairLimit = 1
	transition := Transition{State: state}
	if err := applyEvaluateTurnStep(&transition, state, EvaluateTurnStep{ProgressKey: "same"}); err != nil {
		t.Fatal(err)
	}
	if transition.State.NextAction != StepActionFinalize ||
		transition.State.Convergence.FinalizationAttempted ||
		transition.State.RepairBudgets[RepairDeclaration].Steps != 1 {
		t.Fatalf("missing schema repair opportunity: %+v", transition.State)
	}
	state = transition.State
	state.Convergence.FinalizationAttempted = true
	transition = Transition{State: state}
	if err := applyEvaluateTurnStep(&transition, state, EvaluateTurnStep{ProgressKey: "changed-call-id"}); err != nil {
		t.Fatal(err)
	}
	if transition.State.NextAction != StepActionBlock {
		t.Fatalf("schema repair reset the convergence budget: %+v", transition.State)
	}
}
