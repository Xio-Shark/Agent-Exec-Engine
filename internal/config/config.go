package config

import (
	"fmt"
	"strings"
	"time"

	"github.com/spf13/viper"
)

// Config holds the entire application configuration.
type Config struct {
	Server        ServerConfig        `mapstructure:"server"`
	Redis         RedisConfig         `mapstructure:"redis"`
	DAG           DAGConfig           `mapstructure:"dag"`
	Sandbox       SandboxConfig       `mapstructure:"sandbox"`
	MCP           MCPConfig           `mapstructure:"mcp"`
	Tools         ToolsConfig         `mapstructure:"tools"`
	Observability ObservabilityConfig `mapstructure:"observability"`
	Infra         InfraConfig         `mapstructure:"infra"`
	LLM           LLMConfig           `mapstructure:"llm"`
}

type ServerConfig struct {
	Port         int           `mapstructure:"port"`
	ReadTimeout  time.Duration `mapstructure:"read_timeout"`
	WriteTimeout time.Duration `mapstructure:"write_timeout"`
}

type RedisConfig struct {
	URL      string `mapstructure:"url"`
	PoolSize int    `mapstructure:"pool_size"`
	Password string `mapstructure:"password"`
	DB       int    `mapstructure:"db"`
}

type DAGConfig struct {
	DefaultStepTimeout time.Duration `mapstructure:"default_step_timeout"`
	MaxParallelSteps   int           `mapstructure:"max_parallel_steps"`
	CheckpointInterval int           `mapstructure:"checkpoint_interval"`
}

type SandboxConfig struct {
	DefaultImage  string        `mapstructure:"default_image"`
	CPUQuota      int64         `mapstructure:"cpu_quota"`
	MemoryLimit   int64         `mapstructure:"memory_limit"`
	PidsLimit     int64         `mapstructure:"pids_limit"`
	Timeout       time.Duration `mapstructure:"timeout"`
	NetworkMode   string        `mapstructure:"network_mode"`
	ReadOnlyFS    bool          `mapstructure:"read_only_fs"`
	MaxConcurrent int           `mapstructure:"max_concurrent"`
}

type MCPConfig struct {
	ServerName      string `mapstructure:"server_name"`
	ProtocolVersion string `mapstructure:"protocol_version"`
}

type ToolsConfig struct {
	WebSearch   WebSearchConfig   `mapstructure:"web_search"`
	FileReader  FileReaderConfig  `mapstructure:"file_reader"`
	SQLQuery    SQLQueryConfig    `mapstructure:"sql_query"`
	RAGSearch   RAGSearchConfig   `mapstructure:"rag_search"`
	KnowledgeQA KnowledgeQAConfig `mapstructure:"knowledge_qa"`
}

type WebSearchConfig struct {
	APIKey string `mapstructure:"api_key"`
}

type FileReaderConfig struct {
	BasePath string `mapstructure:"base_path"`
}

type SQLQueryConfig struct {
	DSN string `mapstructure:"dsn"`
}

type RAGSearchConfig struct {
	QdrantURL  string `mapstructure:"qdrant_url"`
	Collection string `mapstructure:"collection"`
	EmbedModel string `mapstructure:"embed_model"`
}

type KnowledgeQAConfig struct {
	RAGServiceURL string `mapstructure:"rag_service_url"`
}

type ObservabilityConfig struct {
	LogLevel     string `mapstructure:"log_level"`
	OTLPEndpoint string `mapstructure:"otlp_endpoint"`
	MetricsPath  string `mapstructure:"metrics_path"`
}

type InfraConfig struct {
	GatewayURL   string `mapstructure:"gateway_url"`
	SchedulerURL string `mapstructure:"scheduler_url"`
}

type LLMConfig struct {
	BaseURL string        `mapstructure:"base_url"`
	Model   string        `mapstructure:"model"`
	APIKey  string        `mapstructure:"api_key"`
	Timeout time.Duration `mapstructure:"timeout"`
}

// Load reads configuration from file and environment variables.
// Environment variables override file values. Prefix: AGENT_EXEC_
// Example: AGENT_EXEC_SERVER_PORT=9090 overrides server.port
func Load(configPath string) (*Config, error) {
	v := viper.New()

	// Defaults
	v.SetDefault("server.port", 8080)
	v.SetDefault("server.read_timeout", "30s")
	v.SetDefault("server.write_timeout", "120s")
	v.SetDefault("redis.url", "redis://localhost:6379")
	v.SetDefault("redis.pool_size", 10)
	v.SetDefault("redis.db", 0)
	v.SetDefault("dag.default_step_timeout", "120s")
	v.SetDefault("dag.max_parallel_steps", 10)
	v.SetDefault("dag.checkpoint_interval", 1)
	v.SetDefault("sandbox.default_image", "python:3.12-slim")
	v.SetDefault("sandbox.cpu_quota", 100000)
	v.SetDefault("sandbox.memory_limit", 268435456) // 256MB
	v.SetDefault("sandbox.pids_limit", 64)
	v.SetDefault("sandbox.timeout", "30s")
	v.SetDefault("sandbox.network_mode", "none")
	v.SetDefault("sandbox.read_only_fs", true)
	v.SetDefault("sandbox.max_concurrent", 5)
	v.SetDefault("mcp.server_name", "agent-exec-engine")
	v.SetDefault("mcp.protocol_version", "2024-11-05")
	v.SetDefault("tools.file_reader.base_path", ".")
	v.SetDefault("tools.rag_search.collection", "default")
	v.SetDefault("tools.rag_search.embed_model", "bge-base-en-v1.5")
	v.SetDefault("tools.knowledge_qa.rag_service_url", "")
	v.SetDefault("observability.log_level", "info")
	v.SetDefault("observability.otlp_endpoint", "localhost:4317")
	v.SetDefault("observability.metrics_path", "/metrics")
	v.SetDefault("infra.gateway_url", "http://localhost:8081")
	v.SetDefault("infra.scheduler_url", "http://localhost:8080")
	v.SetDefault("llm.base_url", "")
	v.SetDefault("llm.model", "qwen7b")
	v.SetDefault("llm.timeout", "60s")

	// Config file
	if configPath != "" {
		v.SetConfigFile(configPath)
	} else {
		v.SetConfigName("config")
		v.SetConfigType("yaml")
		v.AddConfigPath("./configs")
		v.AddConfigPath(".")
	}

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return nil, fmt.Errorf("read config: %w", err)
		}
		// Config file not found is OK — use defaults + env
	}

	// Environment variables: AGENT_EXEC_SERVER_PORT → server.port
	v.SetEnvPrefix("AGENT_EXEC")
	v.SetEnvKeyReplacer(strings.NewReplacer(".", "_"))
	v.AutomaticEnv()

	// Also support bare env vars for common overrides
	_ = v.BindEnv("redis.url", "REDIS_URL")
	_ = v.BindEnv("server.port", "PORT")
	_ = v.BindEnv("observability.otlp_endpoint", "OTLP_ENDPOINT")
	_ = v.BindEnv("infra.gateway_url", "AI_INFRA_GATEWAY_URL")
	_ = v.BindEnv("infra.scheduler_url", "AI_INFRA_SCHEDULER_URL")
	_ = v.BindEnv("llm.base_url", "LLM_BASE_URL")
	_ = v.BindEnv("llm.api_key", "LLM_API_KEY")
	_ = v.BindEnv("tools.web_search.api_key", "TAVILY_API_KEY")
	_ = v.BindEnv("tools.sql_query.dsn", "DATABASE_URL")
	_ = v.BindEnv("tools.file_reader.base_path", "WORKSPACE_ROOT")
	_ = v.BindEnv("tools.rag_search.qdrant_url", "QDRANT_URL")

	var cfg Config
	if err := v.Unmarshal(&cfg); err != nil {
		return nil, fmt.Errorf("unmarshal config: %w", err)
	}
	if cfg.LLM.BaseURL == "" {
		cfg.LLM.BaseURL = joinURL(cfg.Infra.GatewayURL, "/v1")
	}

	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}

	return &cfg, nil
}

// Validate checks config invariants.
func (c *Config) Validate() error {
	if c.Server.Port < 1 || c.Server.Port > 65535 {
		return fmt.Errorf("server.port must be 1-65535, got %d", c.Server.Port)
	}
	if c.DAG.MaxParallelSteps < 1 {
		return fmt.Errorf("dag.max_parallel_steps must be >= 1")
	}
	if c.Sandbox.MemoryLimit < 1024*1024 {
		return fmt.Errorf("sandbox.memory_limit must be >= 1MB")
	}
	if c.Sandbox.MaxConcurrent < 1 {
		return fmt.Errorf("sandbox.max_concurrent must be >= 1")
	}
	if c.Infra.GatewayURL == "" {
		return fmt.Errorf("infra.gateway_url must not be empty")
	}
	if c.Infra.SchedulerURL == "" {
		return fmt.Errorf("infra.scheduler_url must not be empty")
	}
	if c.LLM.BaseURL == "" {
		return fmt.Errorf("llm.base_url must not be empty")
	}
	return nil
}

func joinURL(base, suffix string) string {
	trimmedBase := strings.TrimRight(base, "/")
	trimmedSuffix := strings.TrimLeft(suffix, "/")
	return trimmedBase + "/" + trimmedSuffix
}
