package llm

import "context"

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

const defaultMaxTokens = 4096

// ContextManager provides token-aware context window management for LLM
// conversations. It prevents context overflow by applying one of three
// strategies: Write (sliding window), Select (relevance filtering), or
// Compress (LLM summarization).
//
// Strategy implementations live in separate files:
//   - strategy_write.go   — fitWrite (sliding window)
//   - strategy_select.go  — fitSelect (relevance filtering) + scoring helpers
//   - strategy_compress.go — fitCompress (LLM summarization)
//   - tokens.go           — token estimation utilities
type ContextManager struct {
	maxTokens int
	strategy  ContextStrategy
	client    *Client // used only for Compress strategy
}

// NewContextManager creates a context manager with the specified token budget
// and overflow strategy.
func NewContextManager(maxTokens int, strategy ContextStrategy, client *Client) *ContextManager {
	if maxTokens <= 0 {
		maxTokens = defaultMaxTokens
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
