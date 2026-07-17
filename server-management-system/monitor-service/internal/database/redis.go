package database

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/monitor-service/config"
)

// poolHeadroom covers the scheduler, sampler and fact buffer, which must not
// queue behind the workers.
const poolHeadroom = 16

// ConnectRedis establishes a Redis client connection. The pool must cover every
// worker: BRPOP holds its connection for the whole block, so a smaller pool caps
// real concurrency at the pool size and starves the scheduler.
func ConnectRedis(cfg config.RedisConfig, workers int) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:         cfg.Addr(),
		Password:     cfg.Password,
		DB:           cfg.DB,
		PoolSize:     workers + poolHeadroom,
		MinIdleConns: poolHeadroom,
	})

	// Verify connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("[Redis] Warning: Failed to connect to Redis: %v", err)
		_ = rdb.Close()
		return nil
	} else {
		fmt.Println("[Redis] Connected successfully")
	}

	return rdb
}
