package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

const (
	qaTimeout       = 15 * time.Second
	defaultQADepth  = 3
	defaultQAModel  = "default"
)

// KnowledgeQATool calls a RAG service's /v1/qa/ask endpoint and returns
// the answer with audit_id for traceability (R4).
//
// Unlike rag_search which does vector similarity search directly against Qdrant,
// this tool delegates to the RAG application's full QA pipeline including
// retrieval, generation, and audit logging.
type KnowledgeQATool struct {
	ragBaseURL string
	httpClient *http.Client
}

// NewKnowledgeQATool creates a knowledge QA tool. If ragBaseURL is empty,
// calls return a stub response (safe for environments without a RAG service).
func NewKnowledgeQATool(ragBaseURL string) *KnowledgeQATool {
	return &KnowledgeQATool{
		ragBaseURL: strings.TrimRight(strings.TrimSpace(ragBaseURL), "/"),
		httpClient: &http.Client{Timeout: 20 * time.Second},
	}
}

func (t *KnowledgeQATool) Definition() types.ToolDefinition {
	return types.ToolDefinition{
		Name:        "knowledge_qa",
		Description: "Ask a question to the knowledge base and get a grounded answer with source citations and audit trail. Uses the RAG service's full QA pipeline (retrieval + generation + audit).",
		InputSchema: types.ToolSchema{
			Type: "object",
			Properties: map[string]types.Property{
				"question":  {Type: "string", Description: "The question to ask the knowledge base"},
				"depth":     {Type: "integer", Description: "Retrieval depth: number of documents to consider (default: 3)"},
				"model":     {Type: "string", Description: "RAG model to use (default: 'default')"},
				"dataset":   {Type: "string", Description: "Dataset name to query (default: service default)"},
			},
			Required: []string{"question"},
		},
		RateLimit: 10,
		Category:  "search",
	}
}

func (t *KnowledgeQATool) Handle(ctx context.Context, input map[string]any) (string, error) {
	question, ok := input["question"].(string)
	if !ok || strings.TrimSpace(question) == "" {
		return "", fmt.Errorf("question must be a non-empty string")
	}

	if t.ragBaseURL == "" {
		return fmt.Sprintf("[knowledge_qa] answer for: %s (stub: RAG service not configured, audit_id=N/A)", question), nil
	}

	depth := intOrDefault(input["depth"], defaultQADepth)
	model := stringOrDefault(input["model"], defaultQAModel)
	dataset := stringOrDefault(input["dataset"], "")

	reqBody, err := json.Marshal(map[string]any{
		"question": question,
		"depth":    depth,
		"model":    model,
		"dataset":  dataset,
	})
	if err != nil {
		return "", fmt.Errorf("marshal QA request: %w", err)
	}

	qaCtx, cancel := context.WithTimeout(ctx, qaTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(qaCtx, http.MethodPost, t.ragBaseURL+"/v1/qa/ask", bytes.NewReader(reqBody))
	if err != nil {
		return "", fmt.Errorf("build QA request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	// Propagate X-Request-ID for cross-service tracing (A6).
	if requestID, ok := ctx.Value(requestIDKey{}).(string); ok && requestID != "" {
		req.Header.Set("X-Request-ID", requestID)
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("call QA service: %w", err)
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", fmt.Errorf("read QA response: %w", err)
	}

	if resp.StatusCode >= http.StatusInternalServerError {
		return "", fmt.Errorf("QA service error: status %d: %s", resp.StatusCode, string(body))
	}
	if resp.StatusCode >= http.StatusBadRequest {
		return "", fmt.Errorf("QA request invalid: status %d: %s", resp.StatusCode, string(body))
	}

	var qaResp qaResponse
	if err := json.Unmarshal(body, &qaResp); err != nil {
		return "", fmt.Errorf("decode QA response: %w", err)
	}

	// Build structured output with audit_id for traceability.
	output := qaOutput{
		Answer:    qaResp.Answer,
		AuditID:   qaResp.AuditID,
		Sources:   qaResp.Sources,
		Confidence: qaResp.Confidence,
	}

	outputBytes, err := json.MarshalIndent(output, "", "  ")
	if err != nil {
		return "", fmt.Errorf("marshal QA output: %w", err)
	}
	return string(outputBytes), nil
}

type qaResponse struct {
	Answer     string   `json:"answer"`
	AuditID    string   `json:"audit_id"`
	Sources    []string `json:"sources"`
	Confidence float64  `json:"confidence"`
}

type qaOutput struct {
	Answer     string   `json:"answer"`
	AuditID    string   `json:"audit_id"`
	Sources    []string `json:"sources,omitempty"`
	Confidence float64  `json:"confidence"`
}
