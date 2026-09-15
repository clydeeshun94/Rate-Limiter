package storage_test

import (
	"testing"

	rate "rate-limiter/internal/storage"
)

func TestMemoryStorage_SetAndGet(t *testing.T) {
	s := rate.NewMemoryStorage()
	err := s.Set("key", rate.Record{WindowStart: 100, Count: 1})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	record, exists := s.Get("key")
	if !exists {
		t.Fatal("expected key to exist")
	}
	if record.Count != 1 {
		t.Fatalf("expected count=1, got %d", record.Count)
	}
}

func TestMemoryStorage_Delete(t *testing.T) {
	s := rate.NewMemoryStorage()
	s.Set("key", rate.Record{WindowStart: 100, Count: 1})
	err := s.Delete("key")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	_, exists := s.Get("key")
	if exists {
		t.Fatal("expected key to be deleted")
	}
}
