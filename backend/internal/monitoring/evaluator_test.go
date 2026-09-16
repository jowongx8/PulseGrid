package monitoring

import (
	"testing"
	"time"
)

func TestEvaluateHTTPStatusCodes(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		want       ObservationKind
	}{
		{name: "200", statusCode: 200, want: ObservationHealthy},
		{name: "204", statusCode: 204, want: ObservationHealthy},
		{name: "299", statusCode: 299, want: ObservationHealthy},
		{name: "300", statusCode: 300, want: ObservationHealthy},
		{name: "302", statusCode: 302, want: ObservationHealthy},
		{name: "399", statusCode: 399, want: ObservationHealthy},
		{name: "400", statusCode: 400, want: ObservationIndeterminate},
		{name: "401", statusCode: 401, want: ObservationIndeterminate},
		{name: "403", statusCode: 403, want: ObservationIndeterminate},
		{name: "404", statusCode: 404, want: ObservationIndeterminate},
		{name: "429", statusCode: 429, want: ObservationIndeterminate},
		{name: "499", statusCode: 499, want: ObservationIndeterminate},
		{name: "500", statusCode: 500, want: ObservationFailure},
		{name: "502", statusCode: 502, want: ObservationFailure},
		{name: "503", statusCode: 503, want: ObservationFailure},
		{name: "504", statusCode: 504, want: ObservationFailure},
		{name: "599", statusCode: 599, want: ObservationFailure},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckResult{
				ServiceID:  "github",
				CheckedAt:  time.Date(2026, 9, 17, 1, 2, 3, 0, time.UTC),
				StatusCode: tt.statusCode,
				ErrorKind:  ErrorNone,
			}

			observation := Evaluate(result)
			if observation.Kind != tt.want {
				t.Fatalf("Evaluate() kind = %q, want %q", observation.Kind, tt.want)
			}
		})
	}
}

func TestEvaluateTransportErrors(t *testing.T) {
	tests := []struct {
		name      string
		errorKind ErrorKind
		want      ObservationKind
	}{
		{name: "timeout", errorKind: ErrorTimeout, want: ObservationFailure},
		{name: "dns", errorKind: ErrorDNS, want: ObservationFailure},
		{name: "connection", errorKind: ErrorConnection, want: ObservationFailure},
		{name: "tls", errorKind: ErrorTLS, want: ObservationFailure},
		{name: "canceled", errorKind: ErrorCanceled, want: ObservationIgnored},
		{name: "other", errorKind: ErrorOther, want: ObservationIndeterminate},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckResult{
				ServiceID:  "github",
				CheckedAt:  time.Date(2026, 9, 17, 1, 2, 3, 0, time.UTC),
				StatusCode: 0,
				ErrorKind:  tt.errorKind,
			}

			observation := Evaluate(result)
			if observation.Kind != tt.want {
				t.Fatalf("Evaluate() kind = %q, want %q", observation.Kind, tt.want)
			}
		})
	}
}

func TestEvaluateInvalidInputs(t *testing.T) {
	tests := []struct {
		name       string
		statusCode int
		errorKind  ErrorKind
	}{
		{name: "no status and no error", statusCode: 0, errorKind: ErrorNone},
		{name: "negative status", statusCode: -1, errorKind: ErrorNone},
		{name: "1xx status", statusCode: 199, errorKind: ErrorNone},
		{name: "600 status", statusCode: 600, errorKind: ErrorNone},
		{name: "unknown error kind", statusCode: 0, errorKind: ErrorKind("unexpected")},
		{name: "status and timeout", statusCode: 200, errorKind: ErrorTimeout},
		{name: "status and canceled", statusCode: 503, errorKind: ErrorCanceled},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := CheckResult{
				ServiceID:  "github",
				CheckedAt:  time.Date(2026, 9, 17, 1, 2, 3, 0, time.UTC),
				StatusCode: tt.statusCode,
				ErrorKind:  tt.errorKind,
			}

			observation := Evaluate(result)
			if observation.Kind != ObservationIndeterminate {
				t.Fatalf("Evaluate() kind = %q, want %q", observation.Kind, ObservationIndeterminate)
			}
		})
	}
}

func TestEvaluatePreservesServiceIDAndCheckedAt(t *testing.T) {
	checkedAt := time.Date(2026, 9, 17, 1, 2, 3, 4, time.UTC)
	result := CheckResult{
		ServiceID:  "github",
		CheckedAt:  checkedAt,
		StatusCode: 200,
		ErrorKind:  ErrorNone,
	}

	observation := Evaluate(result)
	if observation.ServiceID != result.ServiceID {
		t.Fatalf("ServiceID = %q, want %q", observation.ServiceID, result.ServiceID)
	}

	if observation.ObservedAt != checkedAt {
		t.Fatalf("ObservedAt = %s, want %s", observation.ObservedAt, checkedAt)
	}
}

func TestEvaluateIsStateless(t *testing.T) {
	result := CheckResult{
		ServiceID:  "github",
		CheckedAt:  time.Date(2026, 9, 17, 1, 2, 3, 0, time.UTC),
		StatusCode: 503,
		ErrorKind:  ErrorNone,
	}

	first := Evaluate(result)
	second := Evaluate(result)
	if first != second {
		t.Fatalf("Evaluate() observations differ: first=%+v second=%+v", first, second)
	}
}
