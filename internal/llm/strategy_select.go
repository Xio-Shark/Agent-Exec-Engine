package llm

import "strings"

type scored struct {
	idx   int
	score float64
	msg   Message
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
