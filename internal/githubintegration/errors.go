package githubintegration

import (
	"errors"
	"time"
)

var (
	ErrInvalid      = errors.New("invalid GitHub reference")
	ErrUnauthorized = errors.New("GitHub credential is unauthorized")
	ErrUnavailable  = errors.New("GitHub resource is unavailable")
	ErrRateLimited  = errors.New("GitHub rate limit exceeded")
)

type RateLimitError struct {
	RetryAt time.Time
}

func (e *RateLimitError) Error() string { return ErrRateLimited.Error() }
func (e *RateLimitError) Unwrap() error { return ErrRateLimited }
