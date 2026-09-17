package monitoring

import "time"

type ErrorKind string

const (
	ErrorNone       ErrorKind = ""
	ErrorTimeout    ErrorKind = "timeout"
	ErrorDNS        ErrorKind = "dns"
	ErrorConnection ErrorKind = "connection"
	ErrorTLS        ErrorKind = "tls"
	ErrorCanceled   ErrorKind = "canceled"
	ErrorOther      ErrorKind = "other"
)

type CheckResult struct {
	ServiceID    string
	CheckedAt    time.Time
	Duration     time.Duration
	StatusCode   int
	ErrorKind    ErrorKind
	ErrorMessage string
}
