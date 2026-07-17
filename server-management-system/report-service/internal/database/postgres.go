package database

import (
	"fmt"
	"log"

	"github.com/vcs-sms/report-service/config"
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

	fmt.Println("[DB] Connected to report_db successfully")
	return db
}
