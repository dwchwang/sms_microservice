package database

import (
	"context"
	"fmt"
	"log"

	"github.com/redis/go-redis/v9"
	"github.com/vcs-sms/auth-service/config"
)

// ConnectRedis establishes a Redis client connection.
func ConnectRedis(cfg config.RedisConfig) *redis.Client {
	rdb := redis.NewClient(&redis.Options{
		Addr:     cfg.Addr(),
		Password: cfg.Password,
		DB:       cfg.DB,
	})

	// Verify connection
	ctx := context.Background()
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Printf("[Redis] Warning: Failed to connect to Redis: %v", err)
	} else {
		fmt.Println("[Redis] Connected successfully")
	}

	return rdb
}
