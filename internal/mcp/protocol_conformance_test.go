package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type protocolTransport string

const (
	protocolTransportHTTP  protocolTransport = "http"
	protocolTransportStdio protocolTransport = "stdio"
)

func TestProtocolConformance_ErrorMatrix(t *testing.T) {
	testCases := []struct {
		name       string
		request    string
		wantCode   int
		wantStatus int
	}{
		{
			name:       "parse error",
			request:    `{"jsonrpc":"2.0","id":"broken","method":"initialize"`,
			wantCode:   ErrParse,
			wantStatus: http.StatusBadRequest,
		},
		{
			name:       "invalid request",
			request:    `{"jsonrpc":"2.0","id":"bad"}`,
			wantCode:   ErrInvalidRequest,
			wantStatus: http.StatusOK,
		},
		{
			name:       "method not found",
			request:    `{"jsonrpc":"2.0","id":"missing","method":"tools/unknown","params":{}}`,
			wantCode:   ErrMethodNotFound,
			wantStatus: http.StatusOK,
		},
		{
			name:       "invalid params",
			request:    `{"jsonrpc":"2.0","id":"bad-params","method":"tools/call","params":{"name":1}}`,
			wantCode:   ErrInvalidParams,
			wantStatus: http.StatusOK,
		},
		{
			name:       "empty batch",
			request:    `[]`,
			wantCode:   ErrInvalidRequest,
			wantStatus: http.StatusOK,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			for _, transport := range []protocolTransport{protocolTransportHTTP, protocolTransportStdio} {
				transport := transport
				t.Run(string(transport), func(t *testing.T) {
					server := NewServer(NewRegistry())

					statusCode, payload := performProtocolRequest(t, transport, server, testCase.request)
					if transport == protocolTransportHTTP && statusCode != testCase.wantStatus {
						t.Fatalf("expected HTTP status %d, got %d", testCase.wantStatus, statusCode)
					}

					response := decodeSingleProtocolResponse(t, payload)
					if response.Error == nil {
						t.Fatal("expected protocol error response")
					}
					if response.Error.Code != testCase.wantCode {
						t.Fatalf("expected error code %d, got %d", testCase.wantCode, response.Error.Code)
					}
				})
			}
		})
	}
}

func TestProtocolConformance_SupportedMethodsAcrossTransports(t *testing.T) {
	request := `[
		{"jsonrpc":"2.0","id":"init","method":"initialize","params":{}},
		{"jsonrpc":"2.0","id":"list","method":"tools/list","params":{}},
		{"jsonrpc":"2.0","id":"call","method":"tools/call","params":{"name":"echo","arguments":{"message":"hello"}}}
	]`

	for _, transport := range []protocolTransport{protocolTransportHTTP, protocolTransportStdio} {
		transport := transport
		t.Run(string(transport), func(t *testing.T) {
			registry := NewRegistry()
			mustRegisterEchoTool(t, registry)
			server := NewServer(registry)

			statusCode, payload := performProtocolRequest(t, transport, server, request)
			if transport == protocolTransportHTTP && statusCode != http.StatusOK {
				t.Fatalf("expected HTTP status %d, got %d", http.StatusOK, statusCode)
			}

			responses := decodeBatchProtocolResponses(t, payload)
			if len(responses) != 3 {
				t.Fatalf("expected 3 batch responses, got %d", len(responses))
			}

			initResult := mustMap(t, responses[0].Result)
			if responses[0].Error != nil {
				t.Fatalf("unexpected initialize error: %+v", responses[0].Error)
			}
			if initResult["protocolVersion"] != "2024-11-05" {
				t.Fatalf("unexpected protocolVersion: %v", initResult["protocolVersion"])
			}

			listResult := mustMap(t, responses[1].Result)
			if responses[1].Error != nil {
				t.Fatalf("unexpected tools/list error: %+v", responses[1].Error)
			}
			if len(mustSlice(t, listResult["tools"])) != 1 {
				t.Fatalf("expected 1 tool in tools/list response")
			}

			callResult := mustMap(t, responses[2].Result)
			if responses[2].Error != nil {
				t.Fatalf("unexpected tools/call error: %+v", responses[2].Error)
			}
			content := mustSlice(t, callResult["content"])
			first := mustMap(t, content[0])
			if first["text"] != "hello" {
				t.Fatalf("unexpected tools/call payload: %v", first["text"])
			}
		})
	}
}

func performProtocolRequest(t *testing.T, transport protocolTransport, server *Server, body string) (int, []byte) {
	t.Helper()

	switch transport {
	case protocolTransportHTTP:
		req := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewBufferString(body))
		rec := httptest.NewRecorder()
		server.ServeHTTP(rec, req)
		return rec.Code, bytes.TrimSpace(rec.Body.Bytes())
	case protocolTransportStdio:
		var output bytes.Buffer
		input := strings.ReplaceAll(body, "\n", "")
		input = strings.ReplaceAll(input, "\t", "")
		if !strings.HasSuffix(input, "\n") {
			input += "\n"
		}
		if err := server.ServeStdio(context.Background(), strings.NewReader(input), &output); err != nil {
			t.Fatalf("ServeStdio() error = %v", err)
		}
		return 0, bytes.TrimSpace(output.Bytes())
	default:
		t.Fatalf("unsupported transport %q", transport)
		return 0, nil
	}
}

func decodeSingleProtocolResponse(t *testing.T, payload []byte) JSONRPCResponse {
	t.Helper()

	var response JSONRPCResponse
	if err := json.Unmarshal(payload, &response); err != nil {
		t.Fatalf("decode protocol response: %v", err)
	}
	return response
}

func decodeBatchProtocolResponses(t *testing.T, payload []byte) []JSONRPCResponse {
	t.Helper()

	var responses []JSONRPCResponse
	if err := json.Unmarshal(payload, &responses); err != nil {
		t.Fatalf("decode protocol batch response: %v", err)
	}
	return responses
}
