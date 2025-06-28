package logger_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Feral-File/feralfile-device/components/feral-connectd/logger"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestSentryCore_Write(t *testing.T) {
	// Create an observed core to capture logs
	observedCore, logs := observer.New(zapcore.InfoLevel)

	// Create a mock Sentry config (disabled)
	sentryConfig := &logger.SentryConfig{
		DSN: "", // Empty DSN means disabled
	}

	// Create Sentry core with the observed core
	sentryCore := logger.NewSentryCore(observedCore, sentryConfig)

	// Create logger with the Sentry core
	logger := zap.New(sentryCore)

	// Test different log levels
	logger.Info("This is an info message", zap.String("key", "value"))
	logger.Warn("This is a warning message", zap.Int("number", 42))
	logger.Error("This is an error message", zap.Error(errors.New("test error")))

	// Verify logs were written to the observed core
	entries := logs.All()
	if len(entries) != 3 {
		t.Errorf("Expected 3 log entries, got %d", len(entries))
	}

	// Verify log levels
	expectedLevels := []zapcore.Level{zapcore.InfoLevel, zapcore.WarnLevel, zapcore.ErrorLevel}
	for i, entry := range entries {
		if entry.Level != expectedLevels[i] {
			t.Errorf("Expected level %v, got %v", expectedLevels[i], entry.Level)
		}
	}
}

func TestSentryCore_fieldsToMap(t *testing.T) {
	sentryCore := &logger.SentryCore{}

	fields := []zapcore.Field{
		zap.String("string_field", "test"),
		zap.Int("int_field", 123),
		zap.Bool("bool_field", true),
		zap.Duration("duration_field", time.Second),
		zap.Error(errors.New("test error")),
	}

	result := sentryCore.FieldsToMap(fields)

	// Verify string field
	if result["string_field"] != "test" {
		t.Errorf("Expected string_field to be 'test', got %v", result["string_field"])
	}

	// Verify int field
	if result["int_field"] != int64(123) {
		t.Errorf("Expected int_field to be 123, got %v", result["int_field"])
	}

	// Verify bool field
	if result["bool_field"] != true {
		t.Errorf("Expected bool_field to be true, got %v", result["bool_field"])
	}

	// Verify duration field
	if result["duration_field"] != "1s" {
		t.Errorf("Expected duration_field to be '1s', got %v", result["duration_field"])
	}

	// Verify error field
	if result["error"] != "test error" {
		t.Errorf("Expected error to be 'test error', got %v", result["error"])
	}
}

func TestSentryCore_findErrorField(t *testing.T) {
	sentryCore := &logger.SentryCore{}

	testError := errors.New("test error")
	fields := []zapcore.Field{
		zap.String("string_field", "test"),
		zap.Error(testError),
		zap.Int("int_field", 123),
	}

	foundError := sentryCore.FindErrorField(fields)

	if foundError == nil {
		t.Error("Expected to find an error field, but got nil")
	}

	if foundError.Error() != "test error" {
		t.Errorf("Expected error message 'test error', got %v", foundError.Error())
	}
}

func TestSentryCore_findErrorField_NoError(t *testing.T) {
	sentryCore := &logger.SentryCore{}

	fields := []zapcore.Field{
		zap.String("string_field", "test"),
		zap.Int("int_field", 123),
	}

	foundError := sentryCore.FindErrorField(fields)

	if foundError != nil {
		t.Errorf("Expected no error field, but got %v", foundError)
	}
}
