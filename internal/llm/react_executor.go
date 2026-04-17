package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go.opentelemetry.io/otel/trace"

	"github.com/Xio-Shark/agent-exec-engine/internal/mcp"
	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

const (
	// maxReActRounds limits the Thought→Action→Observation loop.
	maxReActRounds = 15

	// ReAct system prompt instructs the LLM to follow the Thought/Action/Observation pattern.
	reactSystemPrompt = `You are an autonomous reasoning agent that solves tasks using the ReAct (Reason + Act) paradigm.

For each step, you MUST follow this exact format:

Thought: <your reasoning about what to do next, what you've learned, and what remains>
Action: <the tool name to call, or "finish" if the task is complete>
Action Input: <JSON arguments for the tool, or the final answer if Action is "finish">

Rules:
1. Always start with a Thought before taking an Action.
2. After each tool result (Observation), produce a new Thought.
3. When the task is fully answered, use Action: finish with your final answer as Action Input.
4. Be concise in Thoughts but thorough in reasoning.
5. If a tool call fails, reflect on why and try a different approach.`
)

// ReActExecutor implements the ReAct (Reason+Act) paradigm:
// a loop of Thought → Action → Observation until the agent decides to finish.
type ReActExecutor struct {
	client   *Client
	registry *mcp.Registry
	tracer   *observability.Tracer
	metrics  *observability.Metrics
}

// NewReActExecutor creates a new ReAct step executor.
func NewReActExecutor(
	client *Client,
	registry *mcp.Registry,
	tracer *observability.Tracer,
	metrics *observability.Metrics,
) *ReActExecutor {
	return &ReActExecutor{
		client:   client,
		registry: registry,
		tracer:   tracer,
		metrics:  metrics,
	}
}

// Execute runs the ReAct loop: Thought → Action → Observation, repeating until
// the agent outputs Action: finish or the round limit is reached.
func (e *ReActExecutor) Execute(ctx context.Context, step *types.Step, input map[string]any) (string, error) {
	ctx, span := e.startSpan(ctx, step)
	defer span.End()

	systemPrompt := reactSystemPrompt
	if custom := stringFromConfig(step.Config, "system_prompt"); custom != "" {
		systemPrompt = custom + "\n\n" + reactSystemPrompt
	}

	messages := []Message{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: renderUserContent(step.Config, input)},
	}

	var trajectory []ReActStep

	for round := 0; round < maxReActRounds; round++ {
		// Ask the LLM for the next Thought + Action
		response, err := e.client.Chat(ctx, ChatRequest{Messages: messages})
		if err != nil {
			return "", fmt.Errorf("react round %d: chat error: %w", round, err)
		}
		e.recordUsage(step.ID, response.Usage)

		content := response.Choices[0].Message.Content
		messages = append(messages, Message{Role: "assistant", Content: content})

		// Parse the ReAct response
		thought, action, actionInput := parseReActResponse(content)

		reactStep := ReActStep{
			Round:       round + 1,
			Thought:     thought,
			Action:      action,
			ActionInput: actionInput,
		}

		// Check for finish
		if strings.EqualFold(action, "finish") {
			reactStep.Observation = "[Task completed]"
			trajectory = append(trajectory, reactStep)
			return e.buildResult(actionInput, trajectory), nil
		}

		// Execute tool call
		if e.registry == nil {
			return "", fmt.Errorf("react round %d: agent requested tool %q but registry is not configured", round, action)
		}

		arguments := parseActionInput(actionInput)
		result, rpcErr := e.registry.Call(ctx, types.ToolCall{
			ID:       fmt.Sprintf("react-%s-%d", step.ID, round),
			ToolName: action,
			Input:    arguments,
		})

		var observation string
		if rpcErr != nil {
			observation = fmt.Sprintf("Error: %s", rpcErr.Message)
		} else if result.IsError {
			observation = fmt.Sprintf("Error: %s", result.Content)
		} else {
			observation = result.Content
		}

		e.recordToolCall(action, rpcErr != nil || result.IsError)
		reactStep.Observation = observation
		trajectory = append(trajectory, reactStep)

		// Feed observation back to the LLM
		messages = append(messages, Message{
			Role:    "user",
			Content: fmt.Sprintf("Observation: %s", observation),
		})
	}

	return "", fmt.Errorf("react loop exceeded %d rounds without finishing", maxReActRounds)
}

// ReActStep records one iteration of the ReAct loop.
type ReActStep struct {
	Round       int    `json:"round"`
	Thought     string `json:"thought"`
	Action      string `json:"action"`
	ActionInput string `json:"action_input"`
	Observation string `json:"observation"`
}

// parseReActResponse extracts Thought, Action, and Action Input from the LLM output.
func parseReActResponse(content string) (thought, action, actionInput string) {
	lines := strings.Split(content, "\n")
	var currentField string
	var thoughtLines, actionInputLines []string

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(trimmed, "Thought:"):
			currentField = "thought"
			thoughtLines = append(thoughtLines, strings.TrimPrefix(trimmed, "Thought:"))
		case strings.HasPrefix(trimmed, "Action:"):
			currentField = "action"
			action = strings.TrimSpace(strings.TrimPrefix(trimmed, "Action:"))
		case strings.HasPrefix(trimmed, "Action Input:"):
			currentField = "action_input"
			actionInputLines = append(actionInputLines, strings.TrimPrefix(trimmed, "Action Input:"))
		default:
			switch currentField {
			case "thought":
				thoughtLines = append(thoughtLines, trimmed)
			case "action_input":
				actionInputLines = append(actionInputLines, trimmed)
			}
		}
	}

	thought = strings.TrimSpace(strings.Join(thoughtLines, " "))
	actionInput = strings.TrimSpace(strings.Join(actionInputLines, "\n"))
	return
}

// parseActionInput attempts to parse Action Input as JSON; falls back to a single "query" key.
func parseActionInput(raw string) map[string]any {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]any{}
	}
	var result map[string]any
	if err := json.Unmarshal([]byte(raw), &result); err == nil {
		return result
	}
	return map[string]any{"query": raw}
}

// buildResult returns the final answer with trajectory metadata.
func (e *ReActExecutor) buildResult(finalAnswer string, trajectory []ReActStep) string {
	result := map[string]any{
		"answer":     finalAnswer,
		"trajectory": trajectory,
		"rounds":     len(trajectory),
	}
	data, err := json.Marshal(result)
	if err != nil {
		return finalAnswer
	}
	return string(data)
}

func (e *ReActExecutor) recordUsage(stepID string, usage Usage) {
	if e.metrics == nil {
		return
	}
	e.metrics.TokensUsed.WithLabelValues(stepID, "input").Add(float64(usage.PromptTokens))
	e.metrics.TokensUsed.WithLabelValues(stepID, "output").Add(float64(usage.CompletionTokens))
}

func (e *ReActExecutor) recordToolCall(toolName string, isError bool) {
	if e.metrics == nil {
		return
	}
	status := "success"
	if isError {
		status = "error"
	}
	e.metrics.ToolCallsTotal.WithLabelValues(toolName, status).Inc()
}

func (e *ReActExecutor) startSpan(ctx context.Context, step *types.Step) (context.Context, trace.Span) {
	if e.tracer == nil {
		return ctx, trace.SpanFromContext(ctx)
	}
	return e.tracer.StartStepSpan(ctx, step.ID, "react")
}
