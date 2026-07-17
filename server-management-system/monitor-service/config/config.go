package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config holds all configuration for the monitor service.
// Monitoring owns no PostgreSQL data: its inputs are the Redis target
// projection and its outputs are Redis status plus Elasticsearch facts.
type Config struct {
	App     AppConfig
	Redis   RedisConfig
	ES      ESConfig
	Monitor MonitorConfig
	Log     LogConfig
}

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name string
	Port string
	Env  string
}

// RedisConfig holds Redis connection configuration.
type RedisConfig struct {
	Host     string
	Port     string
	Password string
	DB       int
}

// Addr returns the Redis address string.
func (c RedisConfig) Addr() string {
	return fmt.Sprintf("%s:%s", c.Host, c.Port)
}

// ESConfig holds Elasticsearch connection configuration.
type ESConfig struct {
	Addresses   string
	Username    string
	Password    string
	IndexPrefix string
}

// MonitorConfig holds check-loop configuration.
type MonitorConfig struct {
	TCPTimeout   int    // milliseconds for the TCP connect timeout
	TCPDialHost  string // overrides the target IP so the simulator can answer
	WorkerCount  int    // ping goroutines per instance
	FactCapacity int    // max buffered facts before dropping
}

func (c MonitorConfig) Validate() error {
	if c.TCPTimeout <= 0 {
		return fmt.Errorf("MONITOR_TCP_TIMEOUT must be > 0")
	}
	if c.WorkerCount <= 0 {
		return fmt.Errorf("MONITOR_WORKER_COUNT must be > 0")
	}
	if c.FactCapacity <= 0 {
		return fmt.Errorf("MONITOR_FACT_CAPACITY must be > 0")
	}
	return nil
}

// LogConfig holds logging configuration.
type LogConfig struct {
	Level      string
	Dir        string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

// LoadConfig reads configuration from .env file and environment variables.
func LoadConfig() *Config {
	viper.AutomaticEnv()
	viper.SetConfigFile("../.env")
	_ = viper.ReadInConfig()

	viper.SetDefault("APP_NAME", "monitor-service")
	viper.SetDefault("APP_PORT", "8083")
	viper.SetDefault("APP_ENV", "development")

	viper.SetDefault("REDIS_HOST", "localhost")
	viper.SetDefault("REDIS_PORT", "6379")
	viper.SetDefault("REDIS_PASSWORD", "")
	viper.SetDefault("REDIS_DB", 1)

	viper.SetDefault("ES_ADDRESSES", "http://localhost:9200")
	viper.SetDefault("ES_USERNAME", "")
	viper.SetDefault("ES_PASSWORD", "")
	viper.SetDefault("ES_INDEX_PREFIX", "server-status-logs")

	viper.SetDefault("MONITOR_TCP_TIMEOUT", 3000)
	viper.SetDefault("MONITOR_TCP_DIAL_HOST", "")
	viper.SetDefault("MONITOR_WORKER_COUNT", 200)
	viper.SetDefault("MONITOR_FACT_CAPACITY", 50000)

	viper.SetDefault("LOG_LEVEL", "debug")
	viper.SetDefault("LOG_DIR", "logs/monitor")
	viper.SetDefault("LOG_MAX_SIZE", 100)
	viper.SetDefault("LOG_MAX_BACKUPS", 10)
	viper.SetDefault("LOG_MAX_AGE", 30)
	viper.SetDefault("LOG_COMPRESS", true)

	return &Config{
		App: AppConfig{
			Name: viper.GetString("APP_NAME"),
			Port: viper.GetString("APP_PORT"),
			Env:  viper.GetString("APP_ENV"),
		},
		Redis: RedisConfig{
			Host:     viper.GetString("REDIS_HOST"),
			Port:     viper.GetString("REDIS_PORT"),
			Password: viper.GetString("REDIS_PASSWORD"),
			DB:       viper.GetInt("REDIS_DB"),
		},
		ES: ESConfig{
			Addresses:   viper.GetString("ES_ADDRESSES"),
			Username:    viper.GetString("ES_USERNAME"),
			Password:    viper.GetString("ES_PASSWORD"),
			IndexPrefix: viper.GetString("ES_INDEX_PREFIX"),
		},
		Monitor: MonitorConfig{
			TCPTimeout:   viper.GetInt("MONITOR_TCP_TIMEOUT"),
			TCPDialHost:  viper.GetString("MONITOR_TCP_DIAL_HOST"),
			WorkerCount:  viper.GetInt("MONITOR_WORKER_COUNT"),
			FactCapacity: viper.GetInt("MONITOR_FACT_CAPACITY"),
		},
		Log: LogConfig{
			Level:      viper.GetString("LOG_LEVEL"),
			Dir:        viper.GetString("LOG_DIR"),
			MaxSize:    viper.GetInt("LOG_MAX_SIZE"),
			MaxBackups: viper.GetInt("LOG_MAX_BACKUPS"),
			MaxAge:     viper.GetInt("LOG_MAX_AGE"),
			Compress:   viper.GetBool("LOG_COMPRESS"),
		},
	}
}
