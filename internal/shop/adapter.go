package shop

import (
	"context"
	"errors"
	"time"

	"parfumes_finder/internal/domain"
)

type Adapter interface {
	ID() string
	Search(context.Context, domain.SearchQuery) ([]domain.Offer, error)
	Health(context.Context) HealthResult
}

type HealthStatus string

const (
	HealthHealthy    HealthStatus = "healthy"
	HealthRedirected HealthStatus = "redirected"
	HealthDegraded   HealthStatus = "degraded"
	HealthDown       HealthStatus = "down"
	HealthDisabled   HealthStatus = "disabled"
)

type HealthResult struct {
	Status       HealthStatus
	CanonicalURL string
	Err          error
	CheckedAt    time.Time
}

type ErrorKind string

const (
	ErrorUnknown ErrorKind = "unknown"
	ErrorTimeout ErrorKind = "timeout"
	ErrorAccess  ErrorKind = "access_denied"
	ErrorAntiBot ErrorKind = "anti_bot"
	ErrorParse   ErrorKind = "parse_failure"
)

type AdapterError struct {
	Kind ErrorKind
	Err  error
}

func (err *AdapterError) Error() string {
	return string(err.Kind) + ": " + err.Err.Error()
}

func (err *AdapterError) Unwrap() error {
	return err.Err
}

func NewError(kind ErrorKind, err error) error {
	return &AdapterError{Kind: kind, Err: err}
}

func ErrorKindOf(err error) ErrorKind {
	var adapterErr *AdapterError
	if errors.As(err, &adapterErr) {
		return adapterErr.Kind
	}
	return ErrorUnknown
}
