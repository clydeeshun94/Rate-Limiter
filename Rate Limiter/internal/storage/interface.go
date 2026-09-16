package storage

type Record struct {
	WindowStart int64
	Count       int
	Timestamps  []int64
}

type Storage interface {
	Get(key string) (Record, bool)
	Set(key string, record Record) error
	Delete(key string) error
}