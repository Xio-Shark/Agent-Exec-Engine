package llm

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
