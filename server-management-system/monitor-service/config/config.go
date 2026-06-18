package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config holds all configuration for the monitor service.
type Config struct {
	App       AppConfig
	MonitorDB DatabaseConfig // monitor_schema (health_check_configs)
	ServerDB  DatabaseConfig // server_schema (cross-schema read-only)
	Redis     RedisConfig
	ES        ESConfig
	Kafka     KafkaConfig
	Monitor   MonitorConfig
	Log       LogConfig
}

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name string
	Port string
	Env  string
}

// DatabaseConfig holds PostgreSQL connection configuration.
type DatabaseConfig struct {
	Host     string
	Port     string
	User     string
	Password string
	DBName   string
	Schema   string
	SSLMode  string
}

// DSN returns the PostgreSQL connection string.
func (c DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s search_path=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode, c.Schema,
	)
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
	Addresses string
	Username  string
	Password  string
	IndexName string
}

// KafkaConfig holds Kafka connection configuration.
type KafkaConfig struct {
	Brokers string
}

// MonitorConfig holds health-check specific configuration.
type MonitorConfig struct {
	CheckInterval  int     // seconds between health-check cycles (default: 60)
	TCPTimeout     int     // milliseconds for TCP connect timeout (default: 5000)
	TCPDialHost    string  // optional host override for simulator routing in Docker
	WorkerCount    int     // number of concurrent workers (default: 100)
	DefaultTCPPort int     // default TCP port if not in config (default: 80)
	DefaultUptime  float64 // default uptime rate (default: 0.95)
}

func (c MonitorConfig) Validate() error {
	if c.CheckInterval <= 0 {
		return fmt.Errorf("MONITOR_CHECK_INTERVAL must be > 0")
	}
	if c.TCPTimeout <= 0 {
		return fmt.Errorf("MONITOR_TCP_TIMEOUT must be > 0")
	}
	if c.WorkerCount <= 0 {
		return fmt.Errorf("MONITOR_WORKER_COUNT must be > 0")
	}
	if c.DefaultTCPPort < 1 || c.DefaultTCPPort > 65535 {
		return fmt.Errorf("MONITOR_DEFAULT_TCP_PORT must be between 1 and 65535")
	}
	if c.DefaultUptime < 0 || c.DefaultUptime > 1 {
		return fmt.Errorf("MONITOR_DEFAULT_UPTIME must be between 0 and 1")
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

	// Try to load .env from project root (non-fatal if missing)
	viper.SetConfigFile("../.env")
	_ = viper.ReadInConfig()

	// Set defaults
	viper.SetDefault("APP_NAME", "monitor-service")
	viper.SetDefault("APP_PORT", "8083")
	viper.SetDefault("APP_ENV", "development")

	// Monitor DB (monitor_schema)
	viper.SetDefault("MONITOR_DB_HOST", "localhost")
	viper.SetDefault("MONITOR_DB_PORT", "5432")
	viper.SetDefault("MONITOR_DB_USER", "monitor_user")
	viper.SetDefault("MONITOR_DB_PASSWORD", "monitor_pass_secret")
	viper.SetDefault("MONITOR_DB_NAME", "vcs_sms")
	viper.SetDefault("MONITOR_DB_SCHEMA", "monitor_schema")
	viper.SetDefault("MONITOR_DB_SSLMODE", "disable")

	// Server DB (server_schema — read-only cross-schema access)
	viper.SetDefault("SERVER_DB_HOST", "localhost")
	viper.SetDefault("SERVER_DB_PORT", "5432")
	viper.SetDefault("SERVER_DB_USER", "monitor_user")
	viper.SetDefault("SERVER_DB_PASSWORD", "monitor_pass_secret")
	viper.SetDefault("SERVER_DB_NAME", "vcs_sms")
	viper.SetDefault("SERVER_DB_SCHEMA", "server_schema")
	viper.SetDefault("SERVER_DB_SSLMODE", "disable")

	// Redis
	viper.SetDefault("REDIS_HOST", "localhost")
	viper.SetDefault("REDIS_PORT", "6379")
	viper.SetDefault("REDIS_PASSWORD", "")
	viper.SetDefault("REDIS_DB", 3)

	// Elasticsearch
	viper.SetDefault("ES_ADDRESSES", "http://localhost:9200")
	viper.SetDefault("ES_USERNAME", "")
	viper.SetDefault("ES_PASSWORD", "")
	viper.SetDefault("ES_INDEX_NAME", "server-status-logs")

	// Kafka
	viper.SetDefault("KAFKA_BROKERS", "localhost:9092")

	// Monitor
	viper.SetDefault("MONITOR_CHECK_INTERVAL", 60)
	viper.SetDefault("MONITOR_TCP_TIMEOUT", 5000)
	viper.SetDefault("MONITOR_TCP_DIAL_HOST", "")
	viper.SetDefault("MONITOR_WORKER_COUNT", 100)
	viper.SetDefault("MONITOR_DEFAULT_TCP_PORT", 80)
	viper.SetDefault("MONITOR_DEFAULT_UPTIME", 0.95)

	// Logging
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
		MonitorDB: DatabaseConfig{
			Host:     viper.GetString("MONITOR_DB_HOST"),
			Port:     viper.GetString("MONITOR_DB_PORT"),
			User:     viper.GetString("MONITOR_DB_USER"),
			Password: viper.GetString("MONITOR_DB_PASSWORD"),
			DBName:   viper.GetString("MONITOR_DB_NAME"),
			Schema:   viper.GetString("MONITOR_DB_SCHEMA"),
			SSLMode:  viper.GetString("MONITOR_DB_SSLMODE"),
		},
		ServerDB: DatabaseConfig{
			Host:     viper.GetString("SERVER_DB_HOST"),
			Port:     viper.GetString("SERVER_DB_PORT"),
			User:     viper.GetString("SERVER_DB_USER"),
			Password: viper.GetString("SERVER_DB_PASSWORD"),
			DBName:   viper.GetString("SERVER_DB_NAME"),
			Schema:   viper.GetString("SERVER_DB_SCHEMA"),
			SSLMode:  viper.GetString("SERVER_DB_SSLMODE"),
		},
		Redis: RedisConfig{
			Host:     viper.GetString("REDIS_HOST"),
			Port:     viper.GetString("REDIS_PORT"),
			Password: viper.GetString("REDIS_PASSWORD"),
			DB:       viper.GetInt("REDIS_DB"),
		},
		ES: ESConfig{
			Addresses: viper.GetString("ES_ADDRESSES"),
			Username:  viper.GetString("ES_USERNAME"),
			Password:  viper.GetString("ES_PASSWORD"),
			IndexName: viper.GetString("ES_INDEX_NAME"),
		},
		Kafka: KafkaConfig{
			Brokers: viper.GetString("KAFKA_BROKERS"),
		},
		Monitor: MonitorConfig{
			CheckInterval:  viper.GetInt("MONITOR_CHECK_INTERVAL"),
			TCPTimeout:     viper.GetInt("MONITOR_TCP_TIMEOUT"),
			TCPDialHost:    viper.GetString("MONITOR_TCP_DIAL_HOST"),
			WorkerCount:    viper.GetInt("MONITOR_WORKER_COUNT"),
			DefaultTCPPort: viper.GetInt("MONITOR_DEFAULT_TCP_PORT"),
			DefaultUptime:  viper.GetFloat64("MONITOR_DEFAULT_UPTIME"),
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
