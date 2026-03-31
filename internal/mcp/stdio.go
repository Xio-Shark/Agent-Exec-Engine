package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
)

// ServeStdio serves MCP requests over stdin/stdout.
func (s *Server) ServeStdio(ctx context.Context, in io.Reader, out io.Writer) error {
	scanner := bufio.NewScanner(in)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	encoder := json.NewEncoder(out)
	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := bytes.TrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}

		var payload any
		if err := json.NewDecoder(bytes.NewReader(line)).Decode(&payload); err != nil {
			if err := encoder.Encode(JSONRPCResponse{
				JSONRPC: "2.0",
				Error:   &JSONRPCError{Code: ErrParse, Message: "parse error"},
			}); err != nil {
				return fmt.Errorf("write parse error response: %w", err)
			}
			continue
		}

		normalized, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("normalize stdio payload: %w", err)
		}
		if err := encoder.Encode(s.handleMessage(ctx, normalized)); err != nil {
			return fmt.Errorf("write stdio response: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("read stdio request: %w", err)
	}
	return nil
}
