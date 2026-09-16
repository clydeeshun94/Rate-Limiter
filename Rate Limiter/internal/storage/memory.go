package storage

import "sync"

type MemoryStorage struct {
	mu      sync.Mutex
	storage map[string]Record
}

func NewMemoryStorage() *MemoryStorage {
	return &MemoryStorage{
		storage: make(map[string]Record),
	}
}

func (s *MemoryStorage) Get(key string) (Record, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	record, exists := s.storage[key]
	return record, exists
}

func (s *MemoryStorage) Set(key string, record Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.storage[key] = record
	return nil
}

func (s *MemoryStorage) Delete(key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.storage, key)
	return nil
}
