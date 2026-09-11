package protocol

import (
	"encoding/json"
	"errors"
)

type PlanPurpose string

const (
	PlanPurposeExecution   PlanPurpose = "execution"
	PlanPurposeDeliverable PlanPurpose = "deliverable"
)

func (p PlanPurpose) Normalize() PlanPurpose {
	if p == "" {
		return PlanPurposeExecution
	}
	return p
}

func (p PlanPurpose) Valid() bool {
	switch p.Normalize() {
	case PlanPurposeExecution, PlanPurposeDeliverable:
		return true
	default:
		return false
	}
}

func PlanPurposeFromBody(body string) (PlanPurpose, error) {
	var document struct {
		Purpose PlanPurpose `json:"purpose"`
	}
	if err := json.Unmarshal([]byte(body), &document); err != nil {
		return "", err
	}
	if !document.Purpose.Valid() {
		return "", errors.New("invalid Plan purpose")
	}
	return document.Purpose.Normalize(), nil
}

func validatePlanPurpose(purpose PlanPurpose, body string, canImplement, canAutopilot bool) error {
	if !purpose.Valid() {
		return errors.New("invalid Plan purpose")
	}
	if body != "" {
		documentPurpose, err := PlanPurposeFromBody(body)
		if err != nil || documentPurpose != purpose.Normalize() {
			return errors.New("Plan purpose does not match its document")
		}
	}
	if purpose == PlanPurposeDeliverable && (canImplement || canAutopilot) {
		return errors.New("deliverable Plan cannot grant execution transitions")
	}
	return nil
}
