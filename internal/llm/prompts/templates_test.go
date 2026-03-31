package prompts

import "testing"

func TestRenderPrompt(t *testing.T) {
	t.Parallel()

	got := RenderPrompt(CoderPrompt, map[string]any{
		"WorkflowName":   "build",
		"StepID":         "step-1",
		"PreviousOutput": "done",
	})

	want := "You are a coding agent. Implement step step-1 for workflow build using the provided tools and previous output: done."
	if got != want {
		t.Fatalf("unexpected prompt render: %q", got)
	}
}
