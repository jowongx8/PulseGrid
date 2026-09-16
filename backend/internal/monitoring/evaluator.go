package monitoring

func Evaluate(result CheckResult) Observation {
	return Observation{
		ServiceID:  result.ServiceID,
		ObservedAt: result.CheckedAt,
		Kind:       classifyObservation(result),
	}
}

func classifyObservation(result CheckResult) ObservationKind {
	if result.ErrorKind != ErrorNone && result.StatusCode != 0 {
		return ObservationIndeterminate
	}

	if result.ErrorKind != ErrorNone {
		return observationKindForError(result.ErrorKind)
	}

	return observationKindForStatus(result.StatusCode)
}

func observationKindForStatus(statusCode int) ObservationKind {
	switch {
	case statusCode >= 200 && statusCode <= 399:
		return ObservationHealthy
	case statusCode >= 500 && statusCode <= 599:
		return ObservationFailure
	default:
		return ObservationIndeterminate
	}
}

func observationKindForError(errorKind ErrorKind) ObservationKind {
	switch errorKind {
	case ErrorTimeout, ErrorDNS, ErrorConnection, ErrorTLS:
		return ObservationFailure
	case ErrorCanceled:
		return ObservationIgnored
	default:
		return ObservationIndeterminate
	}
}
