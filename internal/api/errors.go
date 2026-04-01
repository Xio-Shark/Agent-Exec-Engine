package api

import (
	"net/http"

	"github.com/gin-gonic/gin"
)

// ErrorResponse is the unified error envelope for all API endpoints.
type ErrorResponse struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
	Details string `json:"details,omitempty"`
}

// Predefined error constructors to keep handlers DRY.

func errBadRequest(c *gin.Context, msg string) {
	c.JSON(http.StatusBadRequest, ErrorResponse{
		Code:    http.StatusBadRequest,
		Message: msg,
	})
}

func errNotFound(c *gin.Context, msg string) {
	c.JSON(http.StatusNotFound, ErrorResponse{
		Code:    http.StatusNotFound,
		Message: msg,
	})
}

func errConflict(c *gin.Context, msg string) {
	c.JSON(http.StatusConflict, ErrorResponse{
		Code:    http.StatusConflict,
		Message: msg,
	})
}

func errNotImplemented(c *gin.Context, msg string) {
	c.JSON(http.StatusNotImplemented, ErrorResponse{
		Code:    http.StatusNotImplemented,
		Message: msg,
	})
}

func errInternal(c *gin.Context, msg, details string) {
	c.JSON(http.StatusInternalServerError, ErrorResponse{
		Code:    http.StatusInternalServerError,
		Message: msg,
		Details: details,
	})
}
