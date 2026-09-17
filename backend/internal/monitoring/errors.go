package monitoring

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"net/url"
)

func classifyError(parentContext context.Context, requestContext context.Context, err error) ErrorKind {
	if err == nil {
		return ErrorNone
	}

	if parentContext != nil {
		if errors.Is(parentContext.Err(), context.Canceled) {
			return ErrorCanceled
		}

		if errors.Is(parentContext.Err(), context.DeadlineExceeded) {
			return ErrorTimeout
		}
	}

	if errors.Is(err, context.Canceled) {
		return ErrorCanceled
	}

	if requestContext != nil && errors.Is(requestContext.Err(), context.DeadlineExceeded) {
		return ErrorTimeout
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return ErrorTimeout
	}

	var dnsError *net.DNSError
	if errors.As(err, &dnsError) {
		return ErrorDNS
	}

	if isTLSError(err) {
		return ErrorTLS
	}

	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return ErrorTimeout
	}

	var opError *net.OpError
	if errors.As(err, &opError) {
		return ErrorConnection
	}

	return ErrorOther
}

func isTLSError(err error) bool {
	var certificateVerificationError *tls.CertificateVerificationError
	if errors.As(err, &certificateVerificationError) {
		return true
	}

	var recordHeaderError tls.RecordHeaderError
	if errors.As(err, &recordHeaderError) {
		return true
	}

	var unknownAuthorityError x509.UnknownAuthorityError
	if errors.As(err, &unknownAuthorityError) {
		return true
	}

	var hostnameError x509.HostnameError
	if errors.As(err, &hostnameError) {
		return true
	}

	var certificateInvalidError x509.CertificateInvalidError
	if errors.As(err, &certificateInvalidError) {
		return true
	}

	return false
}

func safeErrorMessage(err error) string {
	var urlError *url.Error
	if !errors.As(err, &urlError) {
		return err.Error()
	}

	sanitized := *urlError
	parsedURL, parseErr := url.Parse(urlError.URL)
	if parseErr == nil {
		parsedURL.User = nil
		sanitized.URL = parsedURL.String()
	}

	return sanitized.Error()
}
