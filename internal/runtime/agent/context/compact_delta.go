package agentcontext

import (
	"crypto/sha256"
	"encoding/hex"
)

type FailureDelta struct {
	Failures []Failure `json:"failures,omitempty"`
}

func (f *Failures) Delta() FailureDelta {
	return FailureDelta{Failures: f.List()}
}

func ApplyFailureDelta(delta FailureDelta) *Failures {
	result := NewFailures()
	for index := len(delta.Failures) - 1; index >= 0; index-- {
		failure := delta.Failures[index]
		if failure.Digest == "" {
			sum := sha256.Sum256([]byte(failure.Reason))
			failure.Digest = hex.EncodeToString(sum[:])
		}
		key := failure.Kind + "\x00" + failure.Name + "\x00" + failure.Digest
		copy := failure
		result.records[key] = &copy
		result.order = append(result.order, key)
	}
	return result
}
