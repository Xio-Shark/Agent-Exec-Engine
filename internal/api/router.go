package api

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"go.uber.org/zap"

	"github.com/Xio-Shark/agent-exec-engine/internal/mcp"
	"github.com/Xio-Shark/agent-exec-engine/internal/store"
)

// RouterConfig holds all dependencies needed to wire the HTTP router.
type RouterConfig struct {
	Manager  *WorkflowManager
	Registry *mcp.Registry
	Store    store.Store // may be nil if Redis is unavailable
	Logger   *zap.Logger
}

// SetupRouter creates the gin engine with all middleware and routes.
func SetupRouter(cfg RouterConfig) *gin.Engine {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), RequestIDMiddleware())
	if cfg.Logger != nil {
		r.Use(LoggerMiddleware(cfg.Logger))
	}

	handler := NewHandler(cfg.Manager, cfg.Registry)

	// Health & metrics
	r.GET("/healthz", healthHandler(cfg.Store))
	r.GET("/metrics", gin.WrapH(promhttp.Handler()))

	// MCP endpoint — reuse existing MCP server as HTTP handler if needed.
	// (MCP server is kept at /mcp via direct handler injection in main.go)

	// REST API v1
	v1 := r.Group("/api/v1")
	{
		// Workflow CRUD
		v1.POST("/workflows", handler.CreateWorkflow)
		v1.GET("/workflows/:id", handler.GetWorkflow)
		v1.POST("/workflows/:id/resume", handler.ResumeWorkflow)
		v1.DELETE("/workflows/:id", handler.CancelWorkflow)
		v1.GET("/workflows/:id/steps", handler.ListSteps)
		v1.GET("/workflows/:id/steps/:step_id", handler.GetStep)

		// Tool management
		v1.POST("/tools", handler.RegisterTool)
		v1.GET("/tools", handler.ListTools)
		v1.DELETE("/tools/:name", handler.UnregisterTool)
	}

	return r
}

func healthHandler(s store.Store) gin.HandlerFunc {
	return func(c *gin.Context) {
		status := "ok"
		if s != nil {
			if err := s.Ping(c.Request.Context()); err != nil {
				status = "degraded"
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"status":  status,
			"version": "0.1.0",
		})
	}
}

// MCPHandlerAdapter wraps the existing MCP http.Handler for gin.
func MCPHandlerAdapter(handler http.Handler) gin.HandlerFunc {
	return func(c *gin.Context) {
		handler.ServeHTTP(c.Writer, c.Request)
	}
}

// ListenAddr builds the listen address string from a port number.
func ListenAddr(port int) string {
	return fmt.Sprintf(":%d", port)
}
