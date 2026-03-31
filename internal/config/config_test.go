package config

import (
	"os"
	"testing"
)

func TestLoad_Defaults(t *testing.T) {
	// No config file, no env — should use defaults
	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load with defaults failed: %v", err)
	}

	if cfg.Server.Port != 8080 {
		t.Errorf("expected port 8080, got %d", cfg.Server.Port)
	}
	if cfg.Redis.PoolSize != 10 {
		t.Errorf("expected pool_size 10, got %d", cfg.Redis.PoolSize)
	}
	if cfg.Sandbox.MemoryLimit != 268435456 {
		t.Errorf("expected memory_limit 256MB, got %d", cfg.Sandbox.MemoryLimit)
	}
	if cfg.MCP.ProtocolVersion != "2024-11-05" {
		t.Errorf("expected protocol version 2024-11-05, got %s", cfg.MCP.ProtocolVersion)
	}
	if cfg.Infra.GatewayURL != "http://localhost:8081" {
		t.Errorf("expected infra gateway default, got %s", cfg.Infra.GatewayURL)
	}
	if cfg.LLM.BaseURL != "http://localhost:8081/v1" {
		t.Errorf("expected llm base_url derived from gateway, got %s", cfg.LLM.BaseURL)
	}
}

func TestLoad_EnvOverride(t *testing.T) {
	os.Setenv("PORT", "9090")
	os.Setenv("REDIS_URL", "redis://custom:6380")
	os.Setenv("AI_INFRA_GATEWAY_URL", "http://gateway.internal:9091")
	defer os.Unsetenv("PORT")
	defer os.Unsetenv("REDIS_URL")
	defer os.Unsetenv("AI_INFRA_GATEWAY_URL")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load with env override failed: %v", err)
	}

	if cfg.Server.Port != 9090 {
		t.Errorf("expected port 9090 from env, got %d", cfg.Server.Port)
	}
	if cfg.Redis.URL != "redis://custom:6380" {
		t.Errorf("expected custom redis URL, got %s", cfg.Redis.URL)
	}
	if cfg.Infra.GatewayURL != "http://gateway.internal:9091" {
		t.Errorf("expected gateway URL from env, got %s", cfg.Infra.GatewayURL)
	}
	if cfg.LLM.BaseURL != "http://gateway.internal:9091/v1" {
		t.Errorf("expected derived llm URL from env gateway, got %s", cfg.LLM.BaseURL)
	}
}

func TestConfig_Validate_InvalidPort(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{Port: 0},
		DAG:     DAGConfig{MaxParallelSteps: 1},
		Sandbox: SandboxConfig{MemoryLimit: 1024 * 1024, MaxConcurrent: 1},
		Infra:   InfraConfig{GatewayURL: "http://gateway", SchedulerURL: "http://scheduler"},
		LLM:     LLMConfig{BaseURL: "http://gateway/v1"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for port 0")
	}
}

func TestConfig_Validate_InvalidSandbox(t *testing.T) {
	cfg := &Config{
		Server:  ServerConfig{Port: 8080},
		DAG:     DAGConfig{MaxParallelSteps: 1},
		Sandbox: SandboxConfig{MemoryLimit: 100, MaxConcurrent: 1}, // < 1MB
		Infra:   InfraConfig{GatewayURL: "http://gateway", SchedulerURL: "http://scheduler"},
		LLM:     LLMConfig{BaseURL: "http://gateway/v1"},
	}
	if err := cfg.Validate(); err == nil {
		t.Error("expected validation error for memory_limit < 1MB")
	}
}
