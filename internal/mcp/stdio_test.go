package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func TestServer_ServeStdio_EndToEnd(t *testing.T) {
	registry := NewRegistry()
	mustRegisterEchoTool(t, registry)
	server := NewServer(registry)

	input := strings.Join([]string{
		`{"jsonrpc":"2.0","id":"init","method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}}`,
		`{"jsonrpc":"2.0","id":"call","method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}`,
	}, "\n")

	var output bytes.Buffer
	if err := server.ServeStdio(context.Background(), strings.NewReader(input), &output); err != nil {
		t.Fatalf("ServeStdio() error = %v", err)
	}

	responses := decodeStdioResponses(t, output.Bytes())
	if len(responses) != 3 {
		t.Fatalf("expected 3 responses, got %d", len(responses))
	}
	if responses[0].Error != nil {
		t.Fatalf("unexpected initialize error: %+v", responses[0].Error)
	}
	if responses[1].Error != nil {
		t.Fatalf("unexpected tools/list error: %+v", responses[1].Error)
	}
	if responses[2].Error != nil {
		t.Fatalf("unexpected tools/call error: %+v", responses[2].Error)
	}

	listResult := mustMap(t, responses[1].Result)
	if len(mustSlice(t, listResult["tools"])) != 1 {
		t.Fatalf("expected 1 tool in stdio list response")
	}

	callResult := mustMap(t, responses[2].Result)
	content := mustSlice(t, callResult["content"])
	first := mustMap(t, content[0])
	if first["text"] != "hello" {
		t.Fatalf("unexpected stdio tool output: %v", first["text"])
	}
}

func TestServer_ServeStdio_ParseError(t *testing.T) {
	server := NewServer(NewRegistry())

	var output bytes.Buffer
	if err := server.ServeStdio(context.Background(), strings.NewReader("{not-json}\n"), &output); err != nil {
		t.Fatalf("ServeStdio() error = %v", err)
	}

	responses := decodeStdioResponses(t, output.Bytes())
	if len(responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(responses))
	}
	if responses[0].Error == nil {
		t.Fatal("expected parse error response")
	}
	if responses[0].Error.Code != ErrParse {
		t.Fatalf("expected parse error code %d, got %d", ErrParse, responses[0].Error.Code)
	}
}

func decodeStdioResponses(t *testing.T, payload []byte) []JSONRPCResponse {
	t.Helper()

	scanner := bufio.NewScanner(bytes.NewReader(payload))
	responses := make([]JSONRPCResponse, 0, 4)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}

		var response JSONRPCResponse
		if err := json.Unmarshal([]byte(line), &response); err != nil {
			t.Fatalf("decode stdio response: %v", err)
		}
		responses = append(responses, response)
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("scan stdio responses: %v", err)
	}
	return responses
}
