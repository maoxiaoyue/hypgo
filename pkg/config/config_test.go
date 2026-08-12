package config

import (
	"testing"
	"time"
)

func TestValidateProtocol(t *testing.T) {
	tests := []struct {
		protocol string
		wantErr  bool
	}{
		{"http1", false},
		{"http2", false},
		{"http3", false},
		{"auto", false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.protocol, func(t *testing.T) {
			err := ValidateProtocol(tt.protocol)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateProtocol(%q) error = %v, wantErr %v", tt.protocol, err, tt.wantErr)
			}
		})
	}
}

func TestValidateLogLevel(t *testing.T) {
	tests := []struct {
		level   string
		wantErr bool
	}{
		{"debug", false},
		{"info", false},
		{"notice", false},
		{"warning", false},
		{"emergency", false},
		{"invalid", true},
		{"", true},
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			err := ValidateLogLevel(tt.level)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateLogLevel(%q) error = %v, wantErr %v", tt.level, err, tt.wantErr)
			}
		})
	}
}

func TestValidateDatabaseDriver(t *testing.T) {
	tests := []struct {
		driver  string
		wantErr bool
	}{
		{"mysql", false},
		{"postgres", false},
		{"tidb", false},
		{"redis", false},
		{"cassandra", false},
		{"scylladb", false},
		{"", false}, // Empty is allowed
		{"invalid", true},
	}

	for _, tt := range tests {
		t.Run(tt.driver, func(t *testing.T) {
			err := ValidateDatabaseDriver(tt.driver)
			if (err != nil) != tt.wantErr {
				t.Errorf("ValidateDatabaseDriver(%q) error = %v, wantErr %v", tt.driver, err, tt.wantErr)
			}
		})
	}
}

func TestConfig_ApplyDefaults(t *testing.T) {
	var c Config
	c.ApplyDefaults()

	// Server defaults
	if c.Server.Addr != ":8080" {
		t.Errorf("Expected Server.Addr = :8080, got %q", c.Server.Addr)
	}
	if c.Server.Protocol != "http2" {
		t.Errorf("Expected Server.Protocol = http2, got %q", c.Server.Protocol)
	}
	// 回歸防護：欄位是 int 秒。曾為 time.Duration 時，預設 30*time.Second
	// 再被 server 乘 time.Second 溢位成負數，導致 http.Server 無讀寫 timeout
	if c.Server.ReadTimeout != 30 {
		t.Errorf("Expected Server.ReadTimeout = 30 (seconds), got %v", c.Server.ReadTimeout)
	}
	if d := time.Duration(c.Server.ReadTimeout) * time.Second; d != 30*time.Second {
		t.Errorf("server-side conversion = %v, want 30s (must stay positive)", d)
	}
	if c.Server.MaxHandlers != 1000 {
		t.Errorf("Expected Server.MaxHandlers = 1000, got %d", c.Server.MaxHandlers)
	}

	// Database defaults
	if c.Database.MaxIdleConns != 10 {
		t.Errorf("Expected Database.MaxIdleConns = 10, got %d", c.Database.MaxIdleConns)
	}
	if c.Database.MaxOpenConns != 100 {
		t.Errorf("Expected Database.MaxOpenConns = 100, got %d", c.Database.MaxOpenConns)
	}
	if c.Database.Redis.Addr != "localhost:6379" {
		t.Errorf("Expected Redis.Addr = localhost:6379, got %q", c.Database.Redis.Addr)
	}

	// Logger defaults
	if c.Logger.Level != "info" {
		t.Errorf("Expected Logger.Level = info, got %q", c.Logger.Level)
	}
	if c.Logger.Output != "stdout" {
		t.Errorf("Expected Logger.Output = stdout, got %q", c.Logger.Output)
	}
	if c.Logger.Rotation.MaxSize != "100MB" {
		t.Errorf("Expected Logger.Rotation.MaxSize = 100MB, got %q", c.Logger.Rotation.MaxSize)
	}
	if c.Logger.Rotation.MaxAge != "7d" {
		t.Errorf("Expected Logger.Rotation.MaxAge = 7d, got %q", c.Logger.Rotation.MaxAge)
	}
	if c.Logger.Rotation.MaxBackups != 10 {
		t.Errorf("Expected Logger.Rotation.MaxBackups = 10, got %d", c.Logger.Rotation.MaxBackups)
	}
}

func TestConfig_Validate(t *testing.T) {
	c := Config{}
	c.ApplyDefaults()

	// Test a valid config
	if err := c.Validate(); err != nil {
		t.Errorf("Expected default config to be valid, got error: %v", err)
	}

	// Test invalid protocol
	cInvalidProto := c
	cInvalidProto.Server.Protocol = "invalid"
	if err := cInvalidProto.Validate(); err == nil {
		t.Errorf("Expected validation to fail for invalid protocol")
	}

	// Test HTTP3 requires TLS
	cHTTP3WithoutTLS := c
	cHTTP3WithoutTLS.Server.Protocol = "http3"
	cHTTP3WithoutTLS.Server.TLS.Enabled = false
	if err := cHTTP3WithoutTLS.Validate(); err == nil {
		t.Errorf("Expected validation to fail for HTTP3 without TLS")
	}

	// Test TLS enabled but no cert/key
	cTLSMissingKey := c
	cTLSMissingKey.Server.TLS.Enabled = true
	if err := cTLSMissingKey.Validate(); err == nil {
		t.Errorf("Expected validation to fail for TLS enabled without cert/key")
	}
}
