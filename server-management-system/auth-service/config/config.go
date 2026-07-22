package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
	"github.com/vcs-sms/shared/pkg/confighelper"
)

// Config holds all configuration for the auth service.
type Config struct {
	App      AppConfig
	Database DatabaseConfig
	Redis    RedisConfig
	JWT      JWTConfig
	Log      LogConfig
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
	SSLMode  string
}

// DSN returns the PostgreSQL connection string.
func (c DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%s user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.DBName, c.SSLMode,
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

// JWTConfig holds JWT token configuration.
type JWTConfig struct {
	Secret              string
	AccessExpiryMinutes int
	RefreshExpiryDays   int
}

// AccessTokenDuration returns the access token lifetime as time.Duration.
func (c JWTConfig) AccessTokenDuration() time.Duration {
	return time.Duration(c.AccessExpiryMinutes) * time.Minute
}

// RefreshTokenDuration returns the refresh token lifetime as time.Duration.
func (c JWTConfig) RefreshTokenDuration() time.Duration {
	return time.Duration(c.RefreshExpiryDays) * 24 * time.Hour
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
	viper.SetDefault("APP_NAME", "auth-service")
	viper.SetDefault("APP_PORT", "8081")
	viper.SetDefault("APP_ENV", "development")

	viper.SetDefault("IDENTITY_DB_HOST", "localhost")
	viper.SetDefault("IDENTITY_DB_PORT", "5432")
	viper.SetDefault("IDENTITY_DB_USER", "identity_user")
	viper.SetDefault("IDENTITY_DB_PASSWORD", "identity_pass_secret")
	viper.SetDefault("IDENTITY_DB_NAME", "identity_db")
	viper.SetDefault("IDENTITY_DB_SSLMODE", "disable")

	viper.SetDefault("REDIS_HOST", "localhost")
	viper.SetDefault("REDIS_PORT", "6379")
	viper.SetDefault("REDIS_PASSWORD", "")
	viper.SetDefault("REDIS_DB", 0)

	viper.SetDefault("JWT_SECRET", "vcs-sms-dev-secret-change-in-production")
	viper.SetDefault("JWT_ACCESS_EXPIRY_MINUTES", 15)
	viper.SetDefault("JWT_REFRESH_EXPIRY_DAYS", 7)

	viper.SetDefault("LOG_LEVEL", "debug")
	viper.SetDefault("LOG_DIR", "logs/auth")
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
			Host:     viper.GetString("IDENTITY_DB_HOST"),
			Port:     viper.GetString("IDENTITY_DB_PORT"),
			User:     viper.GetString("IDENTITY_DB_USER"),
			Password: confighelper.GetStringSecret("IDENTITY_DB_PASSWORD", viper.GetString("IDENTITY_DB_PASSWORD")),
			DBName:   viper.GetString("IDENTITY_DB_NAME"),
			SSLMode:  viper.GetString("IDENTITY_DB_SSLMODE"),
		},
		Redis: RedisConfig{
			Host:     viper.GetString("REDIS_HOST"),
			Port:     viper.GetString("REDIS_PORT"),
			Password: confighelper.GetStringSecret("REDIS_PASSWORD", viper.GetString("REDIS_PASSWORD")),
			DB:       viper.GetInt("REDIS_DB"),
		},
		JWT: JWTConfig{
			Secret:              confighelper.GetStringSecret("JWT_SECRET", viper.GetString("JWT_SECRET")),
			AccessExpiryMinutes: viper.GetInt("JWT_ACCESS_EXPIRY_MINUTES"),
			RefreshExpiryDays:   viper.GetInt("JWT_REFRESH_EXPIRY_DAYS"),
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
