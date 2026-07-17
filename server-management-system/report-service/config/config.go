package config

import (
	"fmt"
	"time"

	"github.com/spf13/viper"
)

// ReportTimezone is the zone report dates are interpreted in.
const ReportTimezone = "Asia/Ho_Chi_Minh"

// Config holds all configuration for the report service.
type Config struct {
	App      AppConfig
	Database DatabaseConfig
	ES       ESConfig
	SMTP     SMTPConfig
	Server   ServerClientConfig
	Report   ReportConfig
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
	Port     int
	Name     string
	User     string
	Password string
	SSLMode  string
}

// DSN returns the PostgreSQL connection string.
func (c DatabaseConfig) DSN() string {
	return fmt.Sprintf(
		"host=%s port=%d user=%s password=%s dbname=%s sslmode=%s",
		c.Host, c.Port, c.User, c.Password, c.Name, c.SSLMode,
	)
}

// ESConfig holds Elasticsearch connection configuration.
type ESConfig struct {
	Addresses   string
	Username    string
	Password    string
	IndexPrefix string
}

// SMTPConfig holds Gmail SMTP configuration.
type SMTPConfig struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	// RecipientDomains limits who reports may be mailed to, so the service
	// cannot be turned into an open relay. Empty allows any domain.
	RecipientDomains string
}

// ServerClientConfig points at the Server Service internal API.
type ServerClientConfig struct {
	BaseURL string
	Timeout time.Duration
}

// ReportConfig holds report rules.
type ReportConfig struct {
	// MaxRangeDays caps how wide a report may be.
	MaxRangeDays int
	// CoverageThresholdPct marks a report degraded below this coverage.
	CoverageThresholdPct float64
	// DailyRecipient receives the scheduled daily report.
	DailyRecipient string
	SnapshotCron   string
	DailyCron      string
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

	viper.SetDefault("APP_NAME", "report-service")
	viper.SetDefault("APP_PORT", "8084")
	viper.SetDefault("APP_ENV", "development")

	viper.SetDefault("REPORT_DB_HOST", "localhost")
	viper.SetDefault("REPORT_DB_PORT", 5432)
	viper.SetDefault("REPORT_DB_USER", "report_user_v2")
	viper.SetDefault("REPORT_DB_PASSWORD", "report_pass_secret_v2")
	viper.SetDefault("REPORT_DB_NAME", "report_db")
	viper.SetDefault("REPORT_DB_SSLMODE", "disable")

	viper.SetDefault("ES_ADDRESSES", "http://localhost:9200")
	viper.SetDefault("ES_USERNAME", "")
	viper.SetDefault("ES_PASSWORD", "")
	viper.SetDefault("ES_INDEX_PREFIX", "server-status-logs")

	viper.SetDefault("SMTP_HOST", "smtp.gmail.com")
	viper.SetDefault("SMTP_PORT", 587)
	viper.SetDefault("SMTP_USERNAME", "")
	viper.SetDefault("SMTP_PASSWORD", "")
	viper.SetDefault("SMTP_FROM", "")
	viper.SetDefault("SMTP_RECIPIENT_DOMAINS", "")

	viper.SetDefault("SERVER_INTERNAL_URL", "http://server-service:8082")
	viper.SetDefault("SERVER_INTERNAL_TIMEOUT", 30)

	viper.SetDefault("REPORT_MAX_RANGE_DAYS", 31)
	viper.SetDefault("REPORT_COVERAGE_THRESHOLD", 95.0)
	viper.SetDefault("REPORT_DAILY_RECIPIENT", "")
	// Snapshot runs at 00:30 for the day before; the daily report at 10:00
	// reads what the snapshot produced.
	viper.SetDefault("REPORT_SNAPSHOT_CRON", "30 0 * * *")
	viper.SetDefault("REPORT_DAILY_CRON", "0 10 * * *")

	viper.SetDefault("LOG_LEVEL", "debug")
	viper.SetDefault("LOG_DIR", "logs/report")
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
			Host:     viper.GetString("REPORT_DB_HOST"),
			Port:     viper.GetInt("REPORT_DB_PORT"),
			User:     viper.GetString("REPORT_DB_USER"),
			Password: viper.GetString("REPORT_DB_PASSWORD"),
			Name:     viper.GetString("REPORT_DB_NAME"),
			SSLMode:  viper.GetString("REPORT_DB_SSLMODE"),
		},
		ES: ESConfig{
			Addresses:   viper.GetString("ES_ADDRESSES"),
			Username:    viper.GetString("ES_USERNAME"),
			Password:    viper.GetString("ES_PASSWORD"),
			IndexPrefix: viper.GetString("ES_INDEX_PREFIX"),
		},
		SMTP: SMTPConfig{
			Host:             viper.GetString("SMTP_HOST"),
			Port:             viper.GetInt("SMTP_PORT"),
			Username:         viper.GetString("SMTP_USERNAME"),
			Password:         viper.GetString("SMTP_PASSWORD"),
			From:             viper.GetString("SMTP_FROM"),
			RecipientDomains: viper.GetString("SMTP_RECIPIENT_DOMAINS"),
		},
		Server: ServerClientConfig{
			BaseURL: viper.GetString("SERVER_INTERNAL_URL"),
			Timeout: time.Duration(viper.GetInt("SERVER_INTERNAL_TIMEOUT")) * time.Second,
		},
		Report: ReportConfig{
			MaxRangeDays:         viper.GetInt("REPORT_MAX_RANGE_DAYS"),
			CoverageThresholdPct: viper.GetFloat64("REPORT_COVERAGE_THRESHOLD"),
			DailyRecipient:       viper.GetString("REPORT_DAILY_RECIPIENT"),
			SnapshotCron:         viper.GetString("REPORT_SNAPSHOT_CRON"),
			DailyCron:            viper.GetString("REPORT_DAILY_CRON"),
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
