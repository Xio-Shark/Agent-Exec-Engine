package mcp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"go.uber.org/zap"
)

// Action determines what happens when a guardrail rule matches.
type Action int

const (
	ActionBlock Action = iota // reject the request
	ActionWarn                // allow but log a warning
	ActionLog                 // silently record
)

// Target specifies whether a rule applies to input, output, or both.
type Target int

const (
	TargetInput  Target = iota
	TargetOutput
	TargetBoth
)

// Rule defines a single security detection pattern.
type Rule struct {
	Name    string
	Pattern *regexp.Regexp
	Action  Action
	Target  Target
}

// Guardrail scans tool inputs and outputs for security threats.
type Guardrail struct {
	rules   []Rule
	logger  *zap.Logger
	metrics *observability.Metrics
}

// NewGuardrail creates a guardrail with the given rules.
func NewGuardrail(rules []Rule, logger *zap.Logger, metrics *observability.Metrics) *Guardrail {
	return &Guardrail{
		rules:   rules,
		logger:  logger,
		metrics: metrics,
	}
}

// DefaultRules returns the built-in security rules.
func DefaultRules() []Rule {
	return []Rule{
		{
			Name:    "prompt_injection_ignore",
			Pattern: regexp.MustCompile(`(?i)(ignore|disregard|forget)\s+(previous|above|all)\s+(instructions|prompts|rules)`),
			Action:  ActionBlock,
			Target:  TargetInput,
		},
		{
			Name:    "prompt_injection_system",
			Pattern: regexp.MustCompile(`(?i)(system\s*prompt|you\s+are\s+now|act\s+as\s+if|new\s+instructions)`),
			Action:  ActionBlock,
			Target:  TargetInput,
		},
		{
			Name:    "prompt_injection_delimiter",
			Pattern: regexp.MustCompile(`(?i)(` + "```" + `system|<\|im_start\|>system|<\|system\|>|\[INST\])`),
			Action:  ActionBlock,
			Target:  TargetInput,
		},
		{
			Name:    "path_traversal",
			Pattern: regexp.MustCompile(`\.\.[\\/]`),
			Action:  ActionBlock,
			Target:  TargetInput,
		},
		{
			Name:    "sensitive_env_leak",
			Pattern: regexp.MustCompile(`(?i)(password|secret|api.?key|token|credential)\s*[:=]\s*\S+`),
			Action:  ActionWarn,
			Target:  TargetOutput,
		},
		{
			Name:    "pii_email",
			Pattern: regexp.MustCompile(`[a-zA-Z0-9._%+\-]+@[a-zA-Z0-9.\-]+\.[a-zA-Z]{2,}`),
			Action:  ActionLog,
			Target:  TargetOutput,
		},
	}
}

// ScanInput checks tool input for security threats.
// Returns an error if any Block rule matches.
func (g *Guardrail) ScanInput(toolName string, input map[string]any) error {
	text := flattenInput(input)
	for _, rule := range g.rules {
		if rule.Target != TargetInput && rule.Target != TargetBoth {
			continue
		}
		if !rule.Pattern.MatchString(text) {
			continue
		}
		switch rule.Action {
		case ActionBlock:
			g.recordBlocked(rule.Name, toolName)
			return fmt.Errorf("security: input blocked by rule %q", rule.Name)
		case ActionWarn:
			g.recordWarned(rule.Name, toolName)
		case ActionLog:
			g.logMatch(rule.Name, toolName, "input")
		}
	}
	return nil
}

// ScanOutput checks tool output for security threats.
// Returns sanitized output (redacted if Block rule matches).
func (g *Guardrail) ScanOutput(toolName string, output string) string {
	for _, rule := range g.rules {
		if rule.Target != TargetOutput && rule.Target != TargetBoth {
			continue
		}
		if !rule.Pattern.MatchString(output) {
			continue
		}
		switch rule.Action {
		case ActionBlock:
			g.recordBlocked(rule.Name, toolName)
			return "[REDACTED: security rule triggered]"
		case ActionWarn:
			g.recordWarned(rule.Name, toolName)
		case ActionLog:
			g.logMatch(rule.Name, toolName, "output")
		}
	}
	return output
}

func (g *Guardrail) recordBlocked(ruleName, toolName string) {
	if g.logger != nil {
		g.logger.Warn("guardrail blocked",
			zap.String("rule", ruleName),
			zap.String("tool", toolName),
		)
	}
	if g.metrics != nil {
		g.metrics.GuardrailBlocked.WithLabelValues(ruleName, toolName).Inc()
	}
}

func (g *Guardrail) recordWarned(ruleName, toolName string) {
	if g.logger != nil {
		g.logger.Warn("guardrail warning",
			zap.String("rule", ruleName),
			zap.String("tool", toolName),
		)
	}
	if g.metrics != nil {
		g.metrics.GuardrailWarned.WithLabelValues(ruleName, toolName).Inc()
	}
}

func (g *Guardrail) logMatch(ruleName, toolName, direction string) {
	if g.logger != nil {
		g.logger.Info("guardrail match",
			zap.String("rule", ruleName),
			zap.String("tool", toolName),
			zap.String("direction", direction),
		)
	}
}

// flattenInput concatenates all string values in the input map.
func flattenInput(input map[string]any) string {
	var parts []string
	for _, v := range input {
		switch val := v.(type) {
		case string:
			parts = append(parts, val)
		case map[string]any:
			data, _ := json.Marshal(val)
			parts = append(parts, string(data))
		}
	}
	return strings.Join(parts, " ")
}
