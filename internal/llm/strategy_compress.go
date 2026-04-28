package llm

import (
	"context"
	"fmt"
	"strings"
)

// fitCompress uses the LLM to summarize older messages into a single message,
// preserving the system prompt and recent messages.
func (cm *ContextManager) fitCompress(ctx context.Context, messages []Message) ([]Message, error) {
	if cm.client == nil {
		// Fall back to write strategy if no client is available.
		return cm.fitWrite(messages), nil
	}

	var system *Message
	rest := make([]Message, 0, len(messages))
	for i := range messages {
		if messages[i].Role == "system" {
			system = &messages[i]
		} else {
			rest = append(rest, messages[i])
		}
	}

	if len(rest) <= 2 {
		return messages, nil
	}

	// Keep the last 2 messages as-is; compress the rest.
	recentCount := 2
	if recentCount > len(rest) {
		recentCount = len(rest)
	}
	toCompress := rest[:len(rest)-recentCount]
	recent := rest[len(rest)-recentCount:]

	summary, err := cm.summarize(ctx, toCompress)
	if err != nil {
		// Fall back to write on compression failure.
		return cm.fitWrite(messages), nil
	}

	result := make([]Message, 0, 4)
	if system != nil {
		result = append(result, *system)
	}
	result = append(result, Message{
		Role:    "assistant",
		Content: "[Context Summary] " + summary,
	})
	result = append(result, recent...)
	return result, nil
}

func (cm *ContextManager) summarize(ctx context.Context, messages []Message) (string, error) {
	var sb strings.Builder
	for _, m := range messages {
		fmt.Fprintf(&sb, "[%s]: %s\n", m.Role, m.Content)
	}

	resp, err := cm.client.Chat(ctx, ChatRequest{
		Messages: []Message{
			{Role: "system", Content: "Summarize the following conversation concisely, preserving key facts, decisions, and tool results. Output only the summary."},
			{Role: "user", Content: sb.String()},
		},
	})
	if err != nil {
		return "", err
	}
	if len(resp.Choices) == 0 {
		return "", fmt.Errorf("empty response from summarization")
	}
	return resp.Choices[0].Message.Content, nil
}
