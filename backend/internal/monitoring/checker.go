package monitoring

import (
	"context"
	"net/http"
	"time"

	"github.com/jowongx8/backend/internal/service"
)

const (
	DefaultRequestTimeout = 10 * time.Second
	UserAgent             = "PulseGrid/1.0 (synthetic monitoring; compatible)"
	AcceptHeader          = "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8"
)

type HTTPChecker struct {
	client  *http.Client
	timeout time.Duration
}

func NewHTTPChecker() *HTTPChecker {
	return NewHTTPCheckerWithClient(nil, DefaultRequestTimeout)
}

func NewHTTPCheckerWithClient(client *http.Client, timeout time.Duration) *HTTPChecker {
	if client == nil {
		client = &http.Client{}
	}

	if timeout <= 0 {
		timeout = DefaultRequestTimeout
	}

	return &HTTPChecker{
		client:  client,
		timeout: timeout,
	}
}

func (c *HTTPChecker) Check(ctx context.Context, svc service.Service) CheckResult {
	startedAt := time.Now()
	result := CheckResult{
		ServiceID: svc.ID,
		CheckedAt: startedAt.UTC(),
	}

	requestContext, cancel := context.WithTimeout(ctx, c.timeout)
	defer cancel()

	request, err := http.NewRequestWithContext(requestContext, http.MethodGet, svc.CheckURL, nil)
	if err != nil {
		result.Duration = time.Since(startedAt)
		result.ErrorKind = classifyError(ctx, requestContext, err)
		result.ErrorMessage = safeErrorMessage(err)
		return result
	}

	request.Header.Set("User-Agent", UserAgent)
	request.Header.Set("Accept", AcceptHeader)

	response, err := c.client.Do(request)
	result.Duration = time.Since(startedAt)
	if err != nil {
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}

		result.ErrorKind = classifyError(ctx, requestContext, err)
		result.ErrorMessage = safeErrorMessage(err)
		return result
	}
	if response.Body != nil {
		defer response.Body.Close()
	}

	result.StatusCode = response.StatusCode
	return result
}
