package storage

import (
	"context"
	"encoding/json"
	"time"

	"github.com/go-redis/redis/v8"
)

type RedisSentinelStorage struct {
	client *redis.Client
	ctx    context.Context
}

func NewRedisSentinelStorage(masterName string, sentinelAddrs []string, password string, db int) *RedisSentinelStorage {
	return &RedisSentinelStorage{
	client: redis.NewFailoverClient(&redis.FailoverOptions{
			MasterName:    masterName,
			SentinelAddrs: sentinelAddrs,
			Password:      password,
			DB:            db,
			PoolSize:      100,
			MinIdleConns:  10,
			MaxConnAge:    30 * time.Second,
		}),
		ctx: context.Background(),
	}
}

func (r *RedisSentinelStorage) Get(key string) (Record, bool, error) {
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
		return Record{}, false, err
	}
	return record, found, nil
}

func (r *RedisSentinelStorage) Set(key string, record Record) error {
	data, err := json.Marshal(record)
	if err != nil {
		return err
	}
	return r.retry(func() error {
		return r.client.Set(r.ctx, key, data, 0).Err()
	})
}

func (r *RedisSentinelStorage) Delete(key string) error {
	return r.retry(func() error {
		return r.client.Del(r.ctx, key).Err()
	})
}

func (r *RedisSentinelStorage) retry(fn func() error) error {
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
