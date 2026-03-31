package mcp

const (
	ErrParse          = -32700
	ErrInvalidRequest = -32600
	ErrMethodNotFound = -32601
	ErrInvalidParams  = -32602
	ErrInternal       = -32603
	ErrToolNotFound   = -32000
	ErrRateLimited    = -32001
	ErrPermission     = -32002
)

// RPCError carries a JSON-RPC code through internal layers.
type RPCError struct {
	Code    int
	Message string
}

func (e *RPCError) Error() string {
	return e.Message
}

func NewRPCError(code int, message string) *RPCError {
	return &RPCError{Code: code, Message: message}
}
