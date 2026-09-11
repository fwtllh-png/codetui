package protocol

import "errors"

type TurnWithdrawRequest struct {
	SessionID string `json:"session_id"`
	TurnID    TurnID `json:"turn_id"`
}

func (r TurnWithdrawRequest) Validate() error {
	if !validProfileIdentifier(r.SessionID) || !validProfileIdentifier(string(r.TurnID)) {
		return errors.New("Turn withdrawal identity is invalid")
	}
	return nil
}

type TurnWithdrawnData struct{}

func (*TurnWithdrawnData) eventKind() EventKind { return EventTurnWithdrawn }
func (*TurnWithdrawnData) validate() error      { return nil }
