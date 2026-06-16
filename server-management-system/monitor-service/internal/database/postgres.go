package database

import (
	"fmt"
	"log"
	"time"

	"github.com/vcs-sms/monitor-service/config"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// Connect establishes a GORM database connection using the provided config.
func Connect(cfg config.DatabaseConfig) *gorm.DB {
	logLevel := logger.Warn
	if cfg.SSLMode == "disable" {
		logLevel = logger.Info
	}

	db, err := gorm.Open(postgres.Open(cfg.DSN()), &gorm.Config{
		Logger: logger.Default.LogMode(logLevel),
	})
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	// Configure connection pool
	sqlDB, err := db.DB()
	if err != nil {
		log.Fatalf("Failed to get underlying DB: %v", err)
	}
	sqlDB.SetMaxOpenConns(25)
	sqlDB.SetMaxIdleConns(10)
	// Recycle connections so the long-running health-check loop never reuses a
	// connection that Postgres/Docker/NAT has silently dropped. Without this,
	// a stale pooled connection makes GetAllActiveServers fail every cycle and
	// the reader appears to "stop updating".
	sqlDB.SetConnMaxLifetime(5 * time.Minute)
	sqlDB.SetConnMaxIdleTime(1 * time.Minute)

	fmt.Println("[DB] Connected to PostgreSQL successfully")
	return db
}
