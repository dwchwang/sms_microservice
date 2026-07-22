package config

import (
	"fmt"

	"github.com/spf13/viper"
	"github.com/vcs-sms/shared/pkg/confighelper"
)

// Config holds all configuration for the server service.
type Config struct {
	App      AppConfig
	Database DatabaseConfig
	Redis    RedisConfig
	Log      LogConfig
	Security SecurityConfig
}

// SecurityConfig holds security-related configuration.
type SecurityConfig struct {
	// Comma-separated networks allowed as monitoring targets; empty denies all.
	CIDRAllowlist string `env:"SERVER_CIDR_ALLOWLIST"`
}

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name string
	Port string
	Env  string
}

// DatabaseConfig holds PostgreSQL connection configuration.
type DatabaseConfig struct {
	Host     string `env:"SERVER_DB_HOST" envDefault:"localhost"`
	Port     int    `env:"SERVER_DB_PORT" envDefault:"5432"`
	Name     string `env:"SERVER_DB_NAME" envDefault:"server_db"`
	User     string `env:"SERVER_DB_USER" envDefault:"server_user_v2"`
	Password string `env:"SERVER_DB_PASSWORD" envDefault:"server_pass_secret_v2"`
	SSLMode  string `env:"SERVER_DB_SSLMODE" envDefault:"disable"`
}

// DSN returns the PostgreSQL connection string.
func (c DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
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
	viper.SetDefault("APP_NAME", "server-service")
	viper.SetDefault("APP_PORT", "8082")
	viper.SetDefault("APP_ENV", "development")

	viper.SetDefault("SERVER_DB_HOST", "localhost")
	viper.SetDefault("SERVER_DB_PORT", 5432)
	viper.SetDefault("SERVER_DB_USER", "server_user_v2")
	viper.SetDefault("SERVER_DB_PASSWORD", "server_pass_secret_v2")
	viper.SetDefault("SERVER_DB_NAME", "server_db")
	viper.SetDefault("SERVER_DB_SSLMODE", "disable")

	viper.SetDefault("REDIS_HOST", "localhost")
	viper.SetDefault("REDIS_PORT", "6379")
	viper.SetDefault("REDIS_PASSWORD", "")
	viper.SetDefault("REDIS_DB", 1)

	viper.SetDefault("SERVER_CIDR_ALLOWLIST", "")

	viper.SetDefault("LOG_LEVEL", "debug")
	viper.SetDefault("LOG_DIR", "logs/server")
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
		Database: DatabaseConfig{
			Host:     viper.GetString("SERVER_DB_HOST"),
			Port:     viper.GetInt("SERVER_DB_PORT"),
			User:     viper.GetString("SERVER_DB_USER"),
			Password: confighelper.GetStringSecret("SERVER_DB_PASSWORD", viper.GetString("SERVER_DB_PASSWORD")),
			Name:     viper.GetString("SERVER_DB_NAME"),
			SSLMode:  viper.GetString("SERVER_DB_SSLMODE"),
		},
		Redis: RedisConfig{
			Host:     viper.GetString("REDIS_HOST"),
			Port:     viper.GetString("REDIS_PORT"),
			Password: confighelper.GetStringSecret("REDIS_PASSWORD", viper.GetString("REDIS_PASSWORD")),
			DB:       viper.GetInt("REDIS_DB"),
		},
		Log: LogConfig{
			Level:      viper.GetString("LOG_LEVEL"),
			Dir:        viper.GetString("LOG_DIR"),
			MaxSize:    viper.GetInt("LOG_MAX_SIZE"),
			MaxBackups: viper.GetInt("LOG_MAX_BACKUPS"),
			MaxAge:     viper.GetInt("LOG_MAX_AGE"),
			Compress:   viper.GetBool("LOG_COMPRESS"),
		},
		Security: SecurityConfig{
			CIDRAllowlist: viper.GetString("SERVER_CIDR_ALLOWLIST"),
		},
	}
}
