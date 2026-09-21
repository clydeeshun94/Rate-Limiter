package policy

import (
	"fmt"
	"time"
)

var (
	ErrInvalidLimit   = fmt.Errorf("limit must be a positive integer")
	ErrInvalidWindow  = fmt.Errorf("window must be a positive duration")
	ErrWindowTooSmall = fmt.Errorf("window must be at least 1 second")
	ErrLimitTooLarge  = fmt.Errorf("limit must not exceed 100000")
)

func Validate(limit int, window time.Duration) error {
	if limit <= 0 {
		return ErrInvalidLimit
	}
	if limit > 100000 {
		return ErrLimitTooLarge
	}
	if window <= 0 {
		return ErrInvalidWindow
	}
	if window < time.Second {
		return ErrWindowTooSmall
	}
	return nil
}
