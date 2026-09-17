package monitoring

import (
	"context"
	"crypto/x509"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jowongx8/backend/internal/service"
)

func TestNewHTTPCheckerUsesDefaultTimeout(t *testing.T) {
	checker := NewHTTPChecker()

	if checker.timeout != 10*time.Second {
		t.Fatalf("timeout = %s, want 10s", checker.timeout)
	}
}

func TestCheckRecordsSuccessfulResponse(t *testing.T) {
	var gotMethod string
	var gotUserAgent string
	var gotAccept string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotMethod = r.Method
		gotUserAgent = r.UserAgent()
		gotAccept = r.Header.Get("Accept")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ignored"))
	}))
	defer server.Close()

	checker := NewHTTPCheckerWithClient(server.Client(), time.Second)
	result := checker.Check(context.Background(), testService(server.URL))

	if result.ServiceID != "test-service" {
		t.Fatalf("ServiceID = %q, want test-service", result.ServiceID)
	}

	if result.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}

	if result.ErrorKind != ErrorNone {
		t.Fatalf("ErrorKind = %q, want %q", result.ErrorKind, ErrorNone)
	}

	if result.ErrorMessage != "" {
		t.Fatalf("ErrorMessage = %q, want empty", result.ErrorMessage)
	}

	if result.Duration <= 0 {
		t.Fatalf("Duration = %s, want positive duration", result.Duration)
	}

	if result.CheckedAt.IsZero() {
		t.Fatal("CheckedAt is zero")
	}

	if result.CheckedAt.Location() != time.UTC {
		t.Fatalf("CheckedAt location = %v, want UTC", result.CheckedAt.Location())
	}

	if gotMethod != http.MethodGet {
		t.Fatalf("method = %q, want GET", gotMethod)
	}

	if gotUserAgent != UserAgent {
		t.Fatalf("User-Agent = %q, want %q", gotUserAgent, UserAgent)
	}

	if gotAccept != AcceptHeader {
		t.Fatalf("Accept = %q, want %q", gotAccept, AcceptHeader)
	}
}

func TestCheckPreservesHTTPErrorStatusesAsRawObservations(t *testing.T) {
	tests := []int{
		http.StatusForbidden,
		http.StatusTooManyRequests,
		http.StatusServiceUnavailable,
	}

	for _, statusCode := range tests {
		t.Run(http.StatusText(statusCode), func(t *testing.T) {
			var requestCount int
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requestCount++
				w.WriteHeader(statusCode)
			}))
			defer server.Close()

			checker := NewHTTPCheckerWithClient(server.Client(), time.Second)
			result := checker.Check(context.Background(), testService(server.URL))

			if requestCount != 1 {
				t.Fatalf("requestCount = %d, want 1", requestCount)
			}

			if result.StatusCode != statusCode {
				t.Fatalf("StatusCode = %d, want %d", result.StatusCode, statusCode)
			}

			if result.ErrorKind != ErrorNone {
				t.Fatalf("ErrorKind = %q, want %q", result.ErrorKind, ErrorNone)
			}

			if result.ErrorMessage != "" {
				t.Fatalf("ErrorMessage = %q, want empty", result.ErrorMessage)
			}
		})
	}
}

func TestCheckFollowsRedirects(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/start", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "/final", http.StatusFound)
	})
	mux.HandleFunc("/final", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	server := httptest.NewServer(mux)
	defer server.Close()

	checker := NewHTTPCheckerWithClient(server.Client(), time.Second)
	result := checker.Check(context.Background(), testService(server.URL+"/start"))

	if result.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}

	if result.ErrorKind != ErrorNone {
		t.Fatalf("ErrorKind = %q, want %q", result.ErrorKind, ErrorNone)
	}
}

func TestCheckUsesCheckURL(t *testing.T) {
	var websiteRequests int
	websiteServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		websiteRequests++
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer websiteServer.Close()

	var checkRequests int
	checkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		checkRequests++
		w.WriteHeader(http.StatusOK)
	}))
	defer checkServer.Close()

	svc := testService(checkServer.URL)
	svc.WebsiteURL = websiteServer.URL
	svc.Enabled = false

	checker := NewHTTPCheckerWithClient(checkServer.Client(), time.Second)
	result := checker.Check(context.Background(), svc)

	if result.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}

	if websiteRequests != 0 {
		t.Fatalf("websiteRequests = %d, want 0", websiteRequests)
	}

	if checkRequests != 1 {
		t.Fatalf("checkRequests = %d, want 1", checkRequests)
	}
}

func TestCheckTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPCheckerWithClient(server.Client(), 10*time.Millisecond)
	result := checker.Check(context.Background(), testService(server.URL))

	if result.StatusCode != 0 {
		t.Fatalf("StatusCode = %d, want 0", result.StatusCode)
	}

	if result.ErrorKind != ErrorTimeout {
		t.Fatalf("ErrorKind = %q, want %q", result.ErrorKind, ErrorTimeout)
	}

	if result.ErrorMessage == "" {
		t.Fatal("ErrorMessage is empty, want underlying timeout error")
	}
}

func TestCheckContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	checker := NewHTTPCheckerWithClient(nil, time.Second)
	result := checker.Check(ctx, testService("http://example.test"))

	if result.StatusCode != 0 {
		t.Fatalf("StatusCode = %d, want 0", result.StatusCode)
	}

	if result.ErrorKind != ErrorCanceled {
		t.Fatalf("ErrorKind = %q, want %q", result.ErrorKind, ErrorCanceled)
	}
}

func TestCheckCallerContextDeadlineExceeded(t *testing.T) {
	ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Nanosecond))
	defer cancel()

	checker := NewHTTPCheckerWithClient(nil, time.Second)
	result := checker.Check(ctx, testService("http://example.test"))

	if result.StatusCode != 0 {
		t.Fatalf("StatusCode = %d, want 0", result.StatusCode)
	}

	if result.ErrorKind != ErrorTimeout {
		t.Fatalf("ErrorKind = %q, want %q", result.ErrorKind, ErrorTimeout)
	}
}

func TestCheckTLSFailure(t *testing.T) {
	server := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	checker := NewHTTPCheckerWithClient(&http.Client{}, time.Second)
	result := checker.Check(context.Background(), testService(server.URL))

	if result.StatusCode != 0 {
		t.Fatalf("StatusCode = %d, want 0", result.StatusCode)
	}

	if result.ErrorKind != ErrorTLS {
		t.Fatalf("ErrorKind = %q, want %q", result.ErrorKind, ErrorTLS)
	}
}

func TestCheckClosesResponseBodyWithoutReadingIt(t *testing.T) {
	body := &closeRecorder{}
	client := &http.Client{
		Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       body,
				Header:     make(http.Header),
				Request:    req,
			}, nil
		}),
	}

	checker := NewHTTPCheckerWithClient(client, time.Second)
	result := checker.Check(context.Background(), testService("https://example.com"))

	if result.StatusCode != http.StatusOK {
		t.Fatalf("StatusCode = %d, want %d", result.StatusCode, http.StatusOK)
	}

	if !body.closed {
		t.Fatal("response body was not closed")
	}

	if body.reads != 0 {
		t.Fatalf("response body reads = %d, want 0", body.reads)
	}
}

func TestClassifyError(t *testing.T) {
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()

	deadlineContext, deadlineCancel := context.WithDeadline(context.Background(), time.Now().Add(-time.Nanosecond))
	defer deadlineCancel()
	<-deadlineContext.Done()

	timeoutContext, timeoutCancel := context.WithTimeout(context.Background(), time.Nanosecond)
	defer timeoutCancel()
	<-timeoutContext.Done()

	tests := []struct {
		name           string
		parentContext  context.Context
		requestContext context.Context
		err            error
		want           ErrorKind
	}{
		{
			name:           "dns",
			parentContext:  context.Background(),
			requestContext: context.Background(),
			err: &url.Error{
				Op:  "Get",
				URL: "http://example.invalid",
				Err: &net.DNSError{
					Err:  "no such host",
					Name: "example.invalid",
				},
			},
			want: ErrorDNS,
		},
		{
			name:           "connection",
			parentContext:  context.Background(),
			requestContext: context.Background(),
			err: &url.Error{
				Op:  "Get",
				URL: "http://127.0.0.1:1",
				Err: &net.OpError{
					Op:  "dial",
					Net: "tcp",
					Err: errors.New("connection refused"),
				},
			},
			want: ErrorConnection,
		},
		{
			name:           "tls",
			parentContext:  context.Background(),
			requestContext: context.Background(),
			err: &url.Error{
				Op:  "Get",
				URL: "https://example.com",
				Err: x509.HostnameError{
					Certificate: &x509.Certificate{},
					Host:        "example.com",
				},
			},
			want: ErrorTLS,
		},
		{
			name:           "timeout from request context",
			parentContext:  context.Background(),
			requestContext: timeoutContext,
			err:            context.DeadlineExceeded,
			want:           ErrorTimeout,
		},
		{
			name:           "timeout from caller context deadline",
			parentContext:  deadlineContext,
			requestContext: context.Background(),
			err:            context.DeadlineExceeded,
			want:           ErrorTimeout,
		},
		{
			name:           "timeout from net error",
			parentContext:  context.Background(),
			requestContext: context.Background(),
			err: &url.Error{
				Op:  "Get",
				URL: "https://example.com",
				Err: timeoutError{},
			},
			want: ErrorTimeout,
		},
		{
			name:           "canceled",
			parentContext:  canceledContext,
			requestContext: canceledContext,
			err:            context.Canceled,
			want:           ErrorCanceled,
		},
		{
			name:           "other",
			parentContext:  context.Background(),
			requestContext: context.Background(),
			err:            errors.New("unexpected request failure"),
			want:           ErrorOther,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifyError(tt.parentContext, tt.requestContext, tt.err); got != tt.want {
				t.Fatalf("classifyError() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSafeErrorMessageRemovesURLCredentials(t *testing.T) {
	err := &url.Error{
		Op:  "Get",
		URL: "https://user:secret@example.com/health",
		Err: errors.New("connection refused"),
	}

	message := safeErrorMessage(err)
	if strings.Contains(message, "secret") {
		t.Fatalf("safeErrorMessage() exposed credentials: %q", message)
	}
}

func testService(checkURL string) service.Service {
	return service.Service{
		ID:         "test-service",
		Name:       "Test Service",
		Category:   service.CategoryDeveloperCloud,
		WebsiteURL: "https://example.com",
		CheckURL:   checkURL,
		Weight:     1,
		Enabled:    true,
	}
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

type closeRecorder struct {
	closed bool
	reads  int
}

func (body *closeRecorder) Read(p []byte) (int, error) {
	body.reads++
	return 0, io.EOF
}

func (body *closeRecorder) Close() error {
	body.closed = true
	return nil
}

var _ net.Error = timeoutError{}

type timeoutError struct{}

func (timeoutError) Error() string {
	return "timeout"
}

func (timeoutError) Timeout() bool {
	return true
}

func (timeoutError) Temporary() bool {
	return false
}
