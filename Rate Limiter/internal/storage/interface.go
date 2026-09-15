package storage

type Storage interface {
	Get(key string) (Record, bool)
	Set(key string, record Record) error
	Delete(key string) error
}