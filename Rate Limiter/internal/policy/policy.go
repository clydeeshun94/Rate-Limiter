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
	ErrCostInvalid    = fmt.Errorf("cost must be a positive integer")
	ErrWindowTooLarge = fmt.Errorf("window must not exceed 24 hours")
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
	if window > 24*time.Hour {
		return ErrWindowTooLarge
	}
	return nil
}

func ValidateCost(cost int) error {
	if cost <= 0 {
		return ErrCostInvalid
	}
	return nil
}
