package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/rs/zerolog"
	"github.com/vcs-sms/monitor-service/config"
	"github.com/vcs-sms/monitor-service/internal/model"
	"github.com/vcs-sms/monitor-service/internal/repository/mocks"
	"github.com/vcs-sms/shared/kafka"
)

func TestHandleServerCreated_Success(t *testing.T) {
	var createdConfig *model.HealthCheckConfig
	mockRepo := &mocks.HealthCheckConfigRepoMock{
		CreateFunc: func(ctx context.Context, config *model.HealthCheckConfig) error {
			createdConfig = config
			return nil
		},
	}

	cfg := config.MonitorConfig{
		DefaultTCPPort: 80,
		DefaultUptime:  0.95,
		TCPTimeout:     5000,
	}

	consumer := NewEventConsumer(mockRepo, cfg, zerolog.Nop())

	event := &kafka.Event{
		EventID:   "evt-1",
		EventType: "server.created",
		Source:    "server-service",
		Data: map[string]interface{}{
			"server_id":   "SRV-00001",
			"server_name": "Test Server",
		},
	}

	err := consumer.HandleServerCreated(context.Background(), event)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if createdConfig == nil {
		t.Fatal("Expected config to be created")
	}
	if createdConfig.ServerID != "SRV-00001" {
		t.Errorf("Expected server_id 'SRV-00001', got '%s'", createdConfig.ServerID)
	}
	if createdConfig.CheckMethod != "tcp" {
		t.Errorf("Expected check_method 'tcp', got '%s'", createdConfig.CheckMethod)
	}
	if !createdConfig.IsEnabled {
		t.Error("Expected IsEnabled to be true")
	}
	if createdConfig.ID == "" {
		t.Error("Expected UUID ID to be set")
	}
	if createdConfig.TCPTimeoutMs != 5000 {
		t.Errorf("Expected TCPTimeoutMs=5000, got %d", createdConfig.TCPTimeoutMs)
	}
}

func TestHandleServerDeleted_Success(t *testing.T) {
	var disabledServerID string
	mockRepo := &mocks.HealthCheckConfigRepoMock{
		DisableByServerIDFunc: func(ctx context.Context, serverID string) error {
			disabledServerID = serverID
			return nil
		},
	}

	cfg := config.MonitorConfig{
		DefaultTCPPort: 80,
		DefaultUptime:  0.95,
	}

	consumer := NewEventConsumer(mockRepo, cfg, zerolog.Nop())

	event := &kafka.Event{
		EventID:   "evt-2",
		EventType: "server.deleted",
		Source:    "server-service",
		Data: map[string]interface{}{
			"server_id": "SRV-00001",
		},
	}

	err := consumer.HandleServerDeleted(context.Background(), event)
	if err != nil {
		t.Errorf("Unexpected error: %v", err)
	}

	if disabledServerID != "SRV-00001" {
		t.Errorf("Expected DisableByServerID called with 'SRV-00001', got '%s'", disabledServerID)
	}
}

func TestHandleServerCreated_InvalidData(t *testing.T) {
	mockRepo := &mocks.HealthCheckConfigRepoMock{}
	cfg := config.MonitorConfig{
		DefaultTCPPort: 80,
		DefaultUptime:  0.95,
	}

	consumer := NewEventConsumer(mockRepo, cfg, zerolog.Nop())

	event := &kafka.Event{
		EventID:   "evt-3",
		EventType: "server.created",
		Source:    "server-service",
		Data:      "invalid data",
	}

	err := consumer.HandleServerCreated(context.Background(), event)
	if err == nil {
		t.Error("Expected error for invalid data type, got nil")
	}
}

func TestHandleServerCreated_MissingServerID(t *testing.T) {
	mockRepo := &mocks.HealthCheckConfigRepoMock{}
	cfg := config.MonitorConfig{
		DefaultTCPPort: 80,
		DefaultUptime:  0.95,
	}

	consumer := NewEventConsumer(mockRepo, cfg, zerolog.Nop())

	event := &kafka.Event{
		EventID:   "evt-4",
		EventType: "server.created",
		Source:    "server-service",
		Data:      map[string]interface{}{},
	}

	err := consumer.HandleServerCreated(context.Background(), event)
	if err == nil {
		t.Error("Expected error for missing server_id, got nil")
	}
}

func TestHandleServerDeleted_InvalidData(t *testing.T) {
	mockRepo := &mocks.HealthCheckConfigRepoMock{}
	cfg := config.MonitorConfig{
		DefaultTCPPort: 80,
		DefaultUptime:  0.95,
	}

	consumer := NewEventConsumer(mockRepo, cfg, zerolog.Nop())

	event := &kafka.Event{
		EventID:   "evt-5",
		EventType: "server.deleted",
		Source:    "server-service",
		Data:      nil,
	}

	err := consumer.HandleServerDeleted(context.Background(), event)
	if err == nil {
		t.Error("Expected error for nil event data, got nil")
	}
}

func TestHandleServerDeleted_MissingServerID(t *testing.T) {
	mockRepo := &mocks.HealthCheckConfigRepoMock{}
	consumer := NewEventConsumer(mockRepo, config.MonitorConfig{}, zerolog.Nop())

	event := &kafka.Event{
		EventID:   "evt-missing-delete",
		EventType: "server.deleted",
		Source:    "server-service",
		Data:      map[string]interface{}{},
	}

	err := consumer.HandleServerDeleted(context.Background(), event)
	if err == nil {
		t.Error("Expected error for missing server_id, got nil")
	}
}

func TestHandleServerDeleted_RepoError(t *testing.T) {
	mockRepo := &mocks.HealthCheckConfigRepoMock{
		DisableByServerIDFunc: func(ctx context.Context, serverID string) error {
			return fmt.Errorf("db connection lost")
		},
	}
	consumer := NewEventConsumer(mockRepo, config.MonitorConfig{}, zerolog.Nop())

	event := &kafka.Event{
		EventID:   "evt-delete-error",
		EventType: "server.deleted",
		Source:    "server-service",
		Data: map[string]interface{}{
			"server_id": "SRV-00001",
		},
	}

	err := consumer.HandleServerDeleted(context.Background(), event)
	if err == nil {
		t.Error("Expected repo error, got nil")
	}
}

func TestHandleServerCreated_RepoError(t *testing.T) {
	mockRepo := &mocks.HealthCheckConfigRepoMock{
		CreateFunc: func(ctx context.Context, config *model.HealthCheckConfig) error {
			return fmt.Errorf("db connection lost")
		},
	}

	cfg := config.MonitorConfig{
		DefaultTCPPort: 80,
		DefaultUptime:  0.95,
	}

	consumer := NewEventConsumer(mockRepo, cfg, zerolog.Nop())

	event := &kafka.Event{
		EventID:   "evt-6",
		EventType: "server.created",
		Source:    "server-service",
		Data: map[string]interface{}{
			"server_id": "SRV-00001",
		},
	}

	err := consumer.HandleServerCreated(context.Background(), event)
	if err == nil {
		t.Error("Expected error from repo, got nil")
	}
}
