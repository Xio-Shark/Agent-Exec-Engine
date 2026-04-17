package llm

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

// ContextStrategy determines how the context window is managed when it
// approaches the token limit.
type ContextStrategy string

const (
	// StrategyWrite appends new messages and drops the oldest non-system
	// messages when the budget is exceeded (sliding window).
	StrategyWrite ContextStrategy = "write"

	// StrategySelect keeps only messages whose content is most relevant
	// to the current query, scored by keyword overlap.
	StrategySelect ContextStrategy = "select"

	// StrategyCompress asks the LLM to summarize older messages into a
	// single condensed message, freeing token budget for new content.
	StrategyCompress ContextStrategy = "compress"
)

type scored struct {
	idx   int
	score float64
	msg   Message
}

// ContextManager provides token-aware context window management for LLM
// conversations. It prevents context overflow by applying one of three
// strategies: Write (sliding window), Select (relevance filtering), or
// Compress (LLM summarization).
type ContextManager struct {
	maxTokens int
	strategy  ContextStrategy
	client    *Client // used only for Compress strategy
}

// NewContextManager creates a context manager with the specified token budget
// and overflow strategy.
func NewContextManager(maxTokens int, strategy ContextStrategy, client *Client) *ContextManager {
	if maxTokens <= 0 {
		maxTokens = 4096
	}
	if strategy == "" {
		strategy = StrategyWrite
	}
	return &ContextManager{
		maxTokens: maxTokens,
		strategy:  strategy,
		client:    client,
	}
}

// Fit takes an ordered slice of messages and returns a trimmed slice that fits
// within the token budget, applying the configured strategy.
func (cm *ContextManager) Fit(ctx context.Context, messages []Message) ([]Message, error) {
	if estimateTokens(messages) <= cm.maxTokens {
		return messages, nil
	}
	switch cm.strategy {
	case StrategyWrite:
		return cm.fitWrite(messages), nil
	case StrategySelect:
		return cm.fitSelect(messages), nil
	case StrategyCompress:
		return cm.fitCompress(ctx, messages)
	default:
		return cm.fitWrite(messages), nil
	}
}

// fitWrite implements a sliding window: preserve the system message and the
// last user message, then keep as many recent messages as fit.
func (cm *ContextManager) fitWrite(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
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

	budget := cm.maxTokens
	if system != nil {
		budget -= estimateMessageTokens(*system)
	}

	// Walk backwards to keep the most recent messages.
	kept := make([]Message, 0, len(rest))
	for i := len(rest) - 1; i >= 0; i-- {
		cost := estimateMessageTokens(rest[i])
		if budget-cost < 0 {
			break
		}
		budget -= cost
		kept = append([]Message{rest[i]}, kept...)
	}

	if system != nil {
		kept = append([]Message{*system}, kept...)
	}
	return kept
}

// fitSelect scores each message by keyword overlap with the last user query
// and keeps the highest-scoring messages within the budget.
func (cm *ContextManager) fitSelect(messages []Message) []Message {
	if len(messages) == 0 {
		return messages
	}

	// Extract the last user message as the query anchor.
	var query string
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			query = messages[i].Content
			break
		}
	}
	queryTokens := tokenize(query)

	var system *Message
	candidates := make([]scored, 0, len(messages))
	for i, m := range messages {
		if m.Role == "system" {
			system = &messages[i]
			continue
		}
		candidates = append(candidates, scored{
			idx:   i,
			score: overlapScore(tokenize(m.Content), queryTokens),
			msg:   m,
		})
	}

	// Sort by score descending, but always keep the last user message.
	sortByScore(candidates)

	budget := cm.maxTokens
	if system != nil {
		budget -= estimateMessageTokens(*system)
	}

	kept := make([]Message, 0)
	for _, c := range candidates {
		cost := estimateMessageTokens(c.msg)
		if budget-cost < 0 {
			continue
		}
		budget -= cost
		kept = append(kept, c.msg)
	}

	// Restore original order.
	sortByIndex(kept, messages)

	if system != nil {
		kept = append([]Message{*system}, kept...)
	}
	return kept
}

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

// estimateTokens returns an approximate token count for a slice of messages.
// Uses the ~4 chars/token heuristic common for English/CJK mixed text.
func estimateTokens(messages []Message) int {
	total := 0
	for _, m := range messages {
		total += estimateMessageTokens(m)
	}
	return total
}

func estimateMessageTokens(m Message) int {
	// ~4 chars per token for English, ~2 chars per token for CJK.
	// Use 3 as a reasonable middle ground, plus overhead per message.
	tokens := len(m.Content)/3 + 4 // 4 tokens overhead per message
	if m.Role == "tool" && m.ToolCallID != "" {
		tokens += 10 // tool call metadata overhead
	}
	if len(m.ToolCalls) > 0 {
		raw, _ := json.Marshal(m.ToolCalls)
		tokens += len(raw) / 4
	}
	return tokens
}

// tokenize splits text into lowercase word tokens for overlap scoring.
func tokenize(text string) map[string]int {
	words := strings.Fields(strings.ToLower(text))
	freq := make(map[string]int, len(words))
	for _, w := range words {
		freq[w]++
	}
	return freq
}

// overlapScore computes Jaccard-like overlap between two token frequency maps.
func overlapScore(a, b map[string]int) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	intersection := 0
	for word, countA := range a {
		if countB, ok := b[word]; ok {
			if countA < countB {
				intersection += countA
			} else {
				intersection += countB
			}
		}
	}
	union := len(a) + len(b) - intersection
	if union == 0 {
		return 0
	}
	return float64(intersection) / float64(union)
}

// sortByScore sorts scored candidates by score descending.
func sortByScore(items []scored) {
	for i := 0; i < len(items); i++ {
		maxIdx := i
		for j := i + 1; j < len(items); j++ {
			if items[j].score > items[maxIdx].score {
				maxIdx = j
			}
		}
		items[i], items[maxIdx] = items[maxIdx], items[i]
	}
}

// sortByIndex restores messages to their original conversation order.
func sortByIndex(kept []Message, original []Message) {
	orderMap := make(map[string]int)
	for i, m := range original {
		key := m.Role + "|" + m.Content
		if _, exists := orderMap[key]; !exists {
			orderMap[key] = i
		}
	}
	for i := 0; i < len(kept); i++ {
		minIdx := i
		for j := i + 1; j < len(kept); j++ {
			keyJ := kept[j].Role + "|" + kept[j].Content
			keyMin := kept[minIdx].Role + "|" + kept[minIdx].Content
			if orderMap[keyJ] < orderMap[keyMin] {
				minIdx = j
			}
		}
		kept[i], kept[minIdx] = kept[minIdx], kept[i]
	}
}
