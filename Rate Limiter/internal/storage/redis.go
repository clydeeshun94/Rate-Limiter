package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
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

func (r *RedisStorage) Get(key string) (Record, bool) {
	val, err := r.client.Get(r.ctx, key).Result()
	if err == redis.Nil {
		return Record{}, false
	}
	if err != nil {
		return Record{}, false
	}

	var record Record
	if err := json.Unmarshal([]byte(val), &record); err != nil {
		return Record{}, false
	}

	return record, true
}

func (r *RedisStorage) Set(key string, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}

	return r.client.Set(r.ctx, key, data, r.ttl).Err()
}

func (r *RedisStorage) Delete(key string) error {
	return r.client.Del(r.ctx, key).Err()
}
