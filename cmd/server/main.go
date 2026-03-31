package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/internal/api"
	"github.com/Xio-Shark/agent-exec-engine/internal/config"
	"github.com/Xio-Shark/agent-exec-engine/internal/mcp"
	"github.com/Xio-Shark/agent-exec-engine/internal/mcp/tools"
	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"github.com/Xio-Shark/agent-exec-engine/internal/sandbox"
	"github.com/Xio-Shark/agent-exec-engine/internal/store"
)

func main() {
	stdioMode := flag.Bool("stdio", false, "serve MCP over stdio instead of HTTP")
	configPath := flag.String("config", "", "path to config file")
	flag.Parse()

	// Load configuration
	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("failed to load config: %v", err)
	}

	// Initialize observability
	logger, err := observability.NewLogger()
	if err != nil {
		log.Fatalf("failed to create logger: %v", err)
	}
	defer logger.Sync()

	metrics := observability.NewMetrics()

	traceCtx, traceCancel := context.WithTimeout(context.Background(), 10*time.Second)
	tracer, err := observability.NewTracer(traceCtx, cfg.MCP.ServerName, cfg.Observability.OTLPEndpoint)
	traceCancel()
	if err != nil {
		logger.Fatal(fmt.Sprintf("failed to create tracer: %v", err))
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := tracer.Shutdown(shutdownCtx); err != nil {
			logger.Warn(fmt.Sprintf("tracer shutdown failed: %v", err))
		}
	}()

	// Initialize Redis store
	var redisStore store.Store
	rs, err := store.NewRedisStore(store.RedisOptions{
		URL:      cfg.Redis.URL,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
		PoolSize: cfg.Redis.PoolSize,
	})
	if err != nil {
		logger.Warn(fmt.Sprintf("redis not available (non-fatal): %v", err))
	} else {
		redisStore = rs
		defer rs.Close()
		logger.Info("redis connected")
	}

	// Initialize sandbox executor
	executor, err := sandbox.NewExecutor(
		sandbox.WithTracer(tracer),
		sandbox.WithMetrics(metrics),
	)
	if err != nil {
		logger.Fatal(fmt.Sprintf("failed to create sandbox executor: %v", err))
	}
	prePullCtx, prePullCancel := context.WithTimeout(context.Background(), 2*time.Minute)
	if err := executor.PrePullImages(prePullCtx, sandbox.DefaultImages()); err != nil {
		logger.Warn(fmt.Sprintf("sandbox image pre-pull failed: %v", err))
	}
	prePullCancel()
	sandboxPool := sandbox.NewPool(cfg.Sandbox.MaxConcurrent, executor)

	// Initialize MCP tool registry with security guardrail
	guardrail := mcp.NewGuardrail(mcp.DefaultRules(), logger.Logger, metrics)
	registry := mcp.NewRegistry(mcp.WithTracer(tracer), mcp.WithGuardrail(guardrail))

	// Register built-in tools
	codeExec := tools.NewCodeExecTool(sandboxPool)
	webSearch := tools.NewWebSearchTool(cfg.Tools.WebSearch.APIKey)
	fileReader := tools.NewFileReaderTool(cfg.Tools.FileReader.BasePath)
	sqlQuery := tools.NewSQLQueryTool(cfg.Tools.SQLQuery.DSN)

	_ = registry.Register(codeExec.Definition(), codeExec.Handle)
	_ = registry.Register(webSearch.Definition(), webSearch.Handle)
	_ = registry.Register(fileReader.Definition(), fileReader.Handle)
	_ = registry.Register(sqlQuery.Definition(), sqlQuery.Handle)

	// Register RAG search tool
	ragSearch := tools.NewRAGSearchTool(cfg.Tools.RAGSearch.QdrantURL, cfg.LLM.BaseURL, cfg.Tools.RAGSearch.EmbedModel)
	_ = registry.Register(ragSearch.Definition(), ragSearch.Handle)

	// Create MCP server
	mcpServer := mcp.NewServer(registry, mcp.WithMetrics(metrics))
	if *stdioMode {
		if err := mcpServer.ServeStdio(context.Background(), os.Stdin, os.Stdout); err != nil {
			logger.Fatal(fmt.Sprintf("stdio server error: %v", err))
		}
		return
	}

	// Create workflow manager with default executors
	executors := api.DefaultExecutors(tracer, metrics)
	manager := api.NewWorkflowManager(executors)

	// Setup gin router (replaces http.NewServeMux)
	router := api.SetupRouter(api.RouterConfig{
		Manager:  manager,
		Registry: registry,
		Store:    redisStore,
		Logger:   logger.Logger,
	})

	// Mount MCP server on /mcp
	router.Any("/mcp", api.MCPHandlerAdapter(mcpServer))

	// Start server
	addr := api.ListenAddr(cfg.Server.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
		IdleTimeout:  60 * time.Second,
	}

	go func() {
		logger.Info(fmt.Sprintf("server starting on %s", addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal(fmt.Sprintf("server error: %v", err))
		}
	}()

	// Graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	<-quit

	logger.Info("shutting down server...")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	executor.Cleanup(ctx)
	if err := srv.Shutdown(ctx); err != nil {
		logger.Fatal(fmt.Sprintf("server shutdown error: %v", err))
	}
	logger.Info("server stopped")
}
