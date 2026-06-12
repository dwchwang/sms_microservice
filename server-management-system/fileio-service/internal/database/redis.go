package database

import (
	"context"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/fileio-service/config"
)

// ConnectRedis establishes a Redis connection.
// Returns nil if Redis is not configured (degraded mode).
func ConnectRedis(cfg config.RedisConfig) *redis.Client {
	if cfg.Host == "" || cfg.Port == "" {
		return nil
	}

	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		DialTimeout:  5 * time.Second,
		ReadTimeout:  3 * time.Second,
		WriteTimeout: 3 * time.Second,
		PoolSize:     20,
		MinIdleConns: 5,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := rdb.Ping(ctx).Err(); err != nil {
		fmt.Printf("[WARN] Redis unavailable: %v — running in degraded mode\n", err)
		return nil
	}

	return rdb
}
