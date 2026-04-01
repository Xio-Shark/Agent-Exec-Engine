package llm

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/trace"

	"github.com/Xio-Shark/agent-exec-engine/internal/mcp"
	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

const maxToolRounds = 10

// GPUAllocator abstracts the scheduler handshake needed by LLM execution.
type GPUAllocator interface {
	RequestGPU(ctx context.Context, taskID string, gpuCount int, memoryMin int64) error
	ReleaseGPU(ctx context.Context, taskID string) error
}

// LLMStepExecutor executes LLM steps with optional tool use and GPU reservations.
type LLMStepExecutor struct {
	client    *Client
	registry  *mcp.Registry
	scheduler GPUAllocator
	tracer    *observability.Tracer
	metrics   *observability.Metrics
}

// NewLLMStepExecutor builds an LLM step executor.
func NewLLMStepExecutor(
	client *Client,
	registry *mcp.Registry,
	scheduler GPUAllocator,
	tracer *observability.Tracer,
	metrics *observability.Metrics,
) *LLMStepExecutor {
	return &LLMStepExecutor{
		client:    client,
		registry:  registry,
		scheduler: scheduler,
		tracer:    tracer,
		metrics:   metrics,
	}
}

// Execute implements dag.StepExecutor.
func (e *LLMStepExecutor) Execute(ctx context.Context, step *types.Step, input map[string]any) (result string, err error) {
	ctx, span := e.startSpan(ctx, step)
	defer span.End()

	reserveErr := e.reserveGPU(ctx, step)
	if reserveErr != nil {
		return "", reserveErr
	}
	defer func() {
		releaseErr := e.releaseGPU(ctx, step)
		if releaseErr != nil && err == nil {
			err = releaseErr
		}
	}()

	messages := buildMessages(step.Config, input)
	tools := definitionsToTools(e.registry)
	return e.runConversation(ctx, step, messages, tools)
}

func (e *LLMStepExecutor) runConversation(
	ctx context.Context,
	step *types.Step,
	messages []Message,
	tools []Tool,
) (string, error) {
	for round := 0; round < maxToolRounds; round++ {
		response, err := e.client.Chat(ctx, ChatRequest{Messages: messages, Tools: tools})
		if err != nil {
			return "", err
		}
		e.recordUsage(step.ID, response.Usage)

		choice := response.Choices[0]
		if choice.FinishReason != "tool_calls" {
			return choice.Message.Content, nil
		}
		if e.registry == nil {
			return "", fmt.Errorf("model requested tool calls but registry is not configured")
		}
		messages, err = e.handleToolCalls(ctx, messages, choice.Message)
		if err != nil {
			return "", err
		}
	}
	return "", fmt.Errorf("tool call loop exceeded %d rounds", maxToolRounds)
}

func (e *LLMStepExecutor) handleToolCalls(ctx context.Context, messages []Message, assistant Message) ([]Message, error) {
	messages = append(messages, assistant)
	for _, toolCall := range assistant.ToolCalls {
		callID := chooseToolCallID(toolCall.ID)
		arguments, err := decodeArguments(toolCall.Function.Arguments)
		if err != nil {
			return nil, err
		}
		result, rpcErr := e.registry.Call(ctx, types.ToolCall{
			ID:       callID,
			ToolName: toolCall.Function.Name,
			Input:    arguments,
		})
		if rpcErr != nil {
			return nil, fmt.Errorf("tool %s rpc error: %s", toolCall.Function.Name, rpcErr.Message)
		}
		e.recordToolCall(toolCall.Function.Name, result.IsError)
		if result.IsError {
			return nil, errors.New(result.Content)
		}
		messages = append(messages, Message{
			Role:       "tool",
			ToolCallID: callID,
			Content:    result.Content,
		})
	}
	return messages, nil
}

func (e *LLMStepExecutor) reserveGPU(ctx context.Context, step *types.Step) error {
	gpuCount := intFromConfig(step.Config, "gpu_count")
	if gpuCount <= 0 || e.scheduler == nil {
		return nil
	}
	return e.scheduler.RequestGPU(ctx, step.ID, gpuCount, int64FromConfig(step.Config, "gpu_memory_min"))
}

func (e *LLMStepExecutor) releaseGPU(ctx context.Context, step *types.Step) error {
	gpuCount := intFromConfig(step.Config, "gpu_count")
	if gpuCount <= 0 || e.scheduler == nil {
		return nil
	}
	return e.scheduler.ReleaseGPU(ctx, step.ID)
}

func (e *LLMStepExecutor) recordUsage(stepID string, usage Usage) {
	if e.metrics == nil {
		return
	}
	e.metrics.TokensUsed.WithLabelValues(stepID, "input").Add(float64(usage.PromptTokens))
	e.metrics.TokensUsed.WithLabelValues(stepID, "output").Add(float64(usage.CompletionTokens))
}

func (e *LLMStepExecutor) recordToolCall(toolName string, isError bool) {
	if e.metrics == nil {
		return
	}
	status := "success"
	if isError {
		status = "error"
	}
	e.metrics.ToolCallsTotal.WithLabelValues(toolName, status).Inc()
}

func (e *LLMStepExecutor) startSpan(ctx context.Context, step *types.Step) (context.Context, trace.Span) {
	if e.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return e.tracer.StartStepSpan(ctx, step.ID, string(step.Type))
}

func buildMessages(config map[string]any, input map[string]any) []Message {
	messages := make([]Message, 0, 2)
	if systemPrompt := stringFromConfig(config, "system_prompt"); systemPrompt != "" {
		messages = append(messages, Message{Role: "system", Content: systemPrompt})
	}
	messages = append(messages, Message{Role: "user", Content: renderUserContent(config, input)})
	return messages
}

func renderUserContent(config map[string]any, input map[string]any) string {
	prompt := stringFromConfig(config, "prompt")
	if len(input) == 0 {
		return prompt
	}
	payload, err := json.Marshal(input)
	if err != nil {
		if prompt != "" {
			return prompt
		}
		return fmt.Sprintf("%v", input)
	}
	if prompt == "" {
		return string(payload)
	}
	return prompt + "\n\nPrevious outputs:\n" + string(payload)
}

func definitionsToTools(registry *mcp.Registry) []Tool {
	if registry == nil {
		return nil
	}
	defs := registry.List()
	tools := make([]Tool, 0, len(defs))
	for _, def := range defs {
		tools = append(tools, Tool{
			Type: "function",
			Function: ToolFunction{
				Name:        def.Name,
				Description: def.Description,
				Parameters: map[string]any{
					"type":       def.InputSchema.Type,
					"properties": def.InputSchema.Properties,
					"required":   def.InputSchema.Required,
				},
			},
		})
	}
	return tools
}

func decodeArguments(raw string) (map[string]any, error) {
	if strings.TrimSpace(raw) == "" {
		return map[string]any{}, nil
	}
	var arguments map[string]any
	if err := json.Unmarshal([]byte(raw), &arguments); err != nil {
		return nil, fmt.Errorf("decode tool arguments: %w", err)
	}
	return arguments, nil
}

func chooseToolCallID(id string) string {
	if id != "" {
		return id
	}
	return uuid.NewString()
}

func stringFromConfig(config map[string]any, key string) string {
	value, _ := config[key].(string)
	return value
}

func intFromConfig(config map[string]any, key string) int {
	switch value := config[key].(type) {
	case int:
		return value
	case int64:
		return int(value)
	case float64:
		return int(value)
	default:
		return 0
	}
}

func int64FromConfig(config map[string]any, key string) int64 {
	switch value := config[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		return int64(value)
	default:
		return 0
	}
}
