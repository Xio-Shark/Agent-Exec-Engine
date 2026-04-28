package api

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"go.uber.org/zap"

	"github.com/Xio-Shark/agent-exec-engine/internal/llm"
)

const headerRequestID = "X-Request-ID"

// RequestIDMiddleware injects a unique request ID into context and response header.
// The request ID is propagated downstream via llm.requestIDKey so that LLM client
// calls (and any other outbound HTTP) carry X-Request-ID automatically (A6).
func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := c.GetHeader(headerRequestID)
		if reqID == "" {
			reqID = uuid.NewString()
		}
		c.Set("request_id", reqID)
		c.Header(headerRequestID, reqID)
		// Inject into context for downstream LLM client X-Request-ID propagation.
		c.Request = c.Request.WithContext(
			context.WithValue(c.Request.Context(), llm.RequestIDKey{}, reqID),
		)
		c.Next()
	}
}

// LoggerMiddleware logs every request with latency and status using zap.
func LoggerMiddleware(logger *zap.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		path := c.Request.URL.Path
		method := c.Request.Method

		c.Next()

		logger.Info("http_request",
			zap.String("method", method),
			zap.String("path", path),
			zap.Int("status", c.Writer.Status()),
			zap.Duration("latency", time.Since(start)),
			zap.String("request_id", c.GetString("request_id")),
			zap.String("client_ip", c.ClientIP()),
		)
	}
}
