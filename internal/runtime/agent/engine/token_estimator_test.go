package engine

import (
	"errors"
	"strings"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	"github.com/fwtllh-png/QCode/internal/adapter/tool"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func estimatorMessage(units int) provider.Message {
	return messageWithText(
		provider.RoleUser, strings.Repeat("a", units*4), 1,
	)
}

func TestCalibratedTokenEstimatorAdoptsObservedRatio(t *testing.T) {
	estimator := &calibratedTokenEstimator{inner: HeuristicTokenEstimator{}}
	base, err := estimator.Estimate([]provider.Message{estimatorMessage(1000)})
	if err != nil || base != 1000 {
		t.Fatalf("uncalibrated estimate = %d, %v", base, err)
	}
	estimator.Observe(1000, 1500)
	scaled, err := estimator.Estimate([]provider.Message{estimatorMessage(1000)})
	if err != nil || scaled != 1500 {
		t.Fatalf("calibrated estimate = %d, %v", scaled, err)
	}
	// Rounding errs tight: a half token rounds up so budgets never loosen.
	estimator.Observe(999, 1499)
	ceil, err := estimator.Estimate([]provider.Message{estimatorMessage(1000)})
	if err != nil || ceil < 1500 {
		t.Fatalf("rounded estimate = %d, %v", ceil, err)
	}
}

func TestCalibratedTokenEstimatorIgnoresImplausibleRatios(t *testing.T) {
	estimator := &calibratedTokenEstimator{inner: HeuristicTokenEstimator{}}
	estimator.Observe(1000, 10000)
	if base, _ := estimator.Estimate([]provider.Message{estimatorMessage(1000)}); base != 1000 {
		t.Fatalf("out-of-range high ratio learned: %d", base)
	}
	estimator.Observe(1000, 10)
	if base, _ := estimator.Estimate([]provider.Message{estimatorMessage(1000)}); base != 1000 {
		t.Fatalf("out-of-range low ratio learned: %d", base)
	}
	estimator.Observe(0, 100)
	estimator.Observe(100, 0)
	// Boundary ratios are exactly the documented envelope.
	estimator.Observe(1000, 250)
	if scaled, _ := estimator.Estimate([]provider.Message{estimatorMessage(1000)}); scaled != 250 {
		t.Fatalf("lower boundary ratio rejected: %d", scaled)
	}
	estimator.Observe(1000, 8000)
	if scaled, _ := estimator.Estimate([]provider.Message{estimatorMessage(1000)}); scaled != 8000 {
		t.Fatalf("upper boundary ratio rejected: %d", scaled)
	}
}

type failingEstimator struct{}

func (failingEstimator) Estimate([]provider.Message) (uint64, error) {
	return 0, errors.New("estimator unavailable")
}

func TestCalibratedTokenEstimatorForwardsInnerErrors(t *testing.T) {
	estimator := &calibratedTokenEstimator{inner: failingEstimator{}}
	estimator.Observe(1000, 1500)
	if _, err := estimator.Estimate([]provider.Message{estimatorMessage(1)}); err == nil {
		t.Fatal("inner estimator error was swallowed")
	}
}

func TestEngineObservationCalibratesSessionEstimator(t *testing.T) {
	engine := newEngine(t, &scriptedProvider{}, tool.NewRegistry(nil, nil))
	engine.observeTokenWindow(
		&protocol.SampleContextData{EstimatedTokens: 1000}, 1500, 0,
	)
	estimate, err := engine.options.TokenEstimator.Estimate(
		[]provider.Message{estimatorMessage(1000)},
	)
	if err != nil {
		t.Fatal(err)
	}
	if estimate != 1500 {
		t.Fatalf("session estimator not calibrated: %d", estimate)
	}
}
