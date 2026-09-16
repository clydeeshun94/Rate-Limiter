package storage_test

import (
	"testing"

	rate "rate-limiter/internal/storage"
)

func TestRedisStorage_New(t *testing.T) {
	s := rate.NewRedisStorage("localhost:6379", "", 0)
	if s == nil {
		t.Fatal("expected non-nil RedisStorage")
	}
}

func TestRedisStorage_WindowsOnly(t *testing.T) {
	s := rate.NewRedisStorage("localhost:6379", "", 0)
	if s == nil {
		t.Fatal("expected non-nil RedisStorage")
	}
	t.Skip("requires running Redis instance")
}
