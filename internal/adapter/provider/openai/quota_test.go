package openai

import (
	"errors"
	"fmt"
	"net/http"
	"testing"

	"github.com/fwtllh-png/QCode/internal/adapter/provider"
	providerwire "github.com/fwtllh-png/QCode/internal/adapter/provider/wire"
	"github.com/fwtllh-png/QCode/internal/runtime/protocol"
)

func TestGLMHTTPFailureUsesStructuredBusinessCodes(t *testing.T) {
	for _, code := range []string{"1113", "1308", "1309", "1310", "1316", "1317", "1318", "1319", "1320", "1321"} {
		for _, encoded := range []string{code, fmt.Sprintf("%q", code)} {
			err := compatibleAdapter(t).ClassifyHTTP(providerwire.HTTPFailure{
				ProviderID: "glm", Status: http.StatusTooManyRequests,
				Body: fmt.Sprintf(`{"error":{"code":%s,"message":"reset at 2026-09-07 19:10:43"}}`, encoded),
			})
			var failure *provider.Failure
			if !errors.As(err, &failure) || failure.Code != provider.FailureQuota {
				t.Fatalf("code %s: %v", encoded, err)
			}
			if protocol.IsRetryable(err) || protocol.CodeOf(err) != protocol.CodeResourceExhausted {
				t.Fatalf("quota is retryable: %v", err)
			}
			if _, retry := (providerwire.RetryPolicy{MaxRetries: 10}).Decide(err, false, 0, false); retry {
				t.Fatal("quota retried")
			}
		}
	}
	for _, test := range []struct{ provider, code string }{
		{"glm", "1302"}, {"glm", "1305"}, {"other", "1308"},
	} {
		err := compatibleAdapter(t).ClassifyHTTP(providerwire.HTTPFailure{
			ProviderID: test.provider, Status: http.StatusTooManyRequests,
			Body: fmt.Sprintf(`{"error":{"code":%q,"message":"quota exceeded"}}`, test.code),
		})
		var failure *provider.Failure
		if !errors.As(err, &failure) || failure.Code != provider.FailureRateLimit {
			t.Fatalf("ambiguous/throttle error: %v", err)
		}
	}
}
