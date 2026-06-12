package config

import (
	"fmt"

	"github.com/spf13/viper"
)

// Config holds all configuration for the fileio service.
type Config struct {
	App      AppConfig
	FileIODB DatabaseConfig
	ServerDB DatabaseConfig
	Redis    RedisConfig
	Kafka    KafkaConfig
	Log      LogConfig
}

// AppConfig holds application-level configuration.
type AppConfig struct {
	Name          string
	Port          string
	Env           string
	UploadDir     string
	MaxFileSizeMB int
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

// KafkaConfig holds Kafka connection configuration.
type KafkaConfig struct {
	Brokers string
}

// LogConfig holds logger configuration.
type LogConfig struct {
	Level      string
	Dir        string
	MaxSize    int
	MaxBackups int
	MaxAge     int
	Compress   bool
}

// LoadConfig reads configuration from environment variables via Viper.
func LoadConfig() *Config {
	viper.SetConfigFile(".env")
	viper.SetConfigType("env")

	viper.AutomaticEnv()
	_ = viper.ReadInConfig()

	viper.SetDefault("FILEIO_PORT", "8085")
	viper.SetDefault("FILEIO_UPLOAD_DIR", "/var/uploads/vcs-sms")
	viper.SetDefault("FILEIO_MAX_FILE_SIZE_MB", "10")
	viper.SetDefault("FILEIO_DB_SSLMODE", "disable")
	viper.SetDefault("SERVER_DB_SSLMODE", "disable")
	viper.SetDefault("REDIS_DB", "3")
	viper.SetDefault("LOG_LEVEL", "info")
	viper.SetDefault("LOG_DIR", "/var/log/vcs-sms/fileio")
	viper.SetDefault("LOG_MAX_SIZE", "100")
	viper.SetDefault("LOG_MAX_BACKUPS", "5")
	viper.SetDefault("LOG_MAX_AGE", "30")
	viper.SetDefault("LOG_COMPRESS", "true")

	cfg := &Config{
		App: AppConfig{
			Name:          "fileio-service",
			Port:          viper.GetString("FILEIO_PORT"),
			Env:           viper.GetString("APP_ENV"),
			UploadDir:     viper.GetString("FILEIO_UPLOAD_DIR"),
			MaxFileSizeMB: viper.GetInt("FILEIO_MAX_FILE_SIZE_MB"),
		},
		FileIODB: DatabaseConfig{
			Host:     viper.GetString("FILEIO_DB_HOST"),
			Port:     viper.GetString("FILEIO_DB_PORT"),
			User:     viper.GetString("FILEIO_DB_USER"),
			Password: viper.GetString("FILEIO_DB_PASSWORD"),
			DBName:   viper.GetString("FILEIO_DB_NAME"),
			Schema:   viper.GetString("FILEIO_DB_SCHEMA"),
			SSLMode:  viper.GetString("FILEIO_DB_SSLMODE"),
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
		Kafka: KafkaConfig{
			Brokers: viper.GetString("KAFKA_BROKERS"),
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
	return cfg
}
