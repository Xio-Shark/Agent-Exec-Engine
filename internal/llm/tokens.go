package llm

import "encoding/json"

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
	const overheadPerMessage = 4
	const toolCallMetadataOverhead = 10
	const charsPerToken = 3

	tokens := len(m.Content)/charsPerToken + overheadPerMessage
	if m.Role == "tool" && m.ToolCallID != "" {
		tokens += toolCallMetadataOverhead
	}
	if len(m.ToolCalls) > 0 {
		raw, _ := json.Marshal(m.ToolCalls)
		tokens += len(raw) / 4
	}
	return tokens
}
