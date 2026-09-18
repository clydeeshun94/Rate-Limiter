package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
)

const (
	maxRetries = 3
	retryDelay = 100 * time.Millisecond
)

type RedisStorage struct {
	client *redis.Client
	ctx    context.Context
	ttl    time.Duration
}

func NewRedisStorage(addr string, password string, db int, ttl time.Duration) *RedisStorage {
	return &RedisStorage{
		client: redis.NewClient(&redis.Options{
			Addr:     addr,
			Password: password,
			DB:       db,
		}),
		ctx: context.Background(),
		ttl: ttl,
	}
}

func (r *RedisStorage) retry(fn func() error) error {
	var err error
	for i := 0; i < maxRetries; i++ {
		err = fn()
		if err == nil {
			return nil
		}
		if i < maxRetries-1 {
			time.Sleep(retryDelay)
		}
	}
	return err
}

func (r *RedisStorage) Get(key string) (Record, bool) {
	var record Record
	var found bool
	err := r.retry(func() error {
		val, err := r.client.Get(r.ctx, key).Result()
		if err == redis.Nil {
			found = false
			return nil
		}
		if err != nil {
			return err
		}
		var rec Record
		if err := json.Unmarshal([]byte(val), &rec); err != nil {
			return err
		}
		record = rec
		found = true
		return nil
	})
	if err != nil {
		return Record{}, false
	}
	return record, found
}

func (r *RedisStorage) Set(key string, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return r.retry(func() error {
		return r.client.Set(r.ctx, key, data, r.ttl).Err()
	})
}

func (r *RedisStorage) Delete(key string) error {
	return r.retry(func() error {
		return r.client.Del(r.ctx, key).Err()
	})
}
