package prompts

import (
	"bytes"
	"text/template"
)

const (
	// PlannerPrompt guides task decomposition.
	PlannerPrompt = `You are a planning agent. Analyze the task and break it into concrete execution steps for workflow {{.WorkflowName}}.`

	// CoderPrompt guides implementation work.
	CoderPrompt = `You are a coding agent. Implement step {{.StepID}} for workflow {{.WorkflowName}} using the provided tools and previous output: {{.PreviousOutput}}.`

	// ReviewerPrompt guides review work.
	ReviewerPrompt = `You are a code reviewer. Review workflow {{.WorkflowName}} step {{.StepID}} and provide concrete feedback based on: {{.PreviousOutput}}.`
)

// RenderPrompt renders a text/template using the provided data.
func RenderPrompt(templateText string, data map[string]any) string {
	tmpl := template.Must(template.New("prompt").Parse(templateText))

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return ""
	}
	return buf.String()
}
