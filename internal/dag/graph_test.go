package dag

import (
	"testing"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestNewGraph_LinearDeps(t *testing.T) {
	steps := []types.Step{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}

	graph, err := NewGraph(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if ready := graph.ReadySteps(); len(ready) != 1 || ready[0] != "a" {
		t.Fatalf("expected only a ready, got %v", ready)
	}
	if ready := graph.MarkComplete("a"); len(ready) != 1 || ready[0] != "b" {
		t.Fatalf("expected b after a, got %v", ready)
	}
	if ready := graph.MarkComplete("b"); len(ready) != 1 || ready[0] != "c" {
		t.Fatalf("expected c after b, got %v", ready)
	}
}

func TestNewGraph_CycleDetection(t *testing.T) {
	steps := []types.Step{
		{ID: "a", DependsOn: []string{"c"}},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "c", DependsOn: []string{"b"}},
	}

	if _, err := NewGraph(steps); err == nil {
		t.Fatal("expected cycle error")
	}
}

func TestGraph_ConditionalBranch(t *testing.T) {
	steps := []types.Step{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}, Condition: `status == "approved"`},
		{ID: "c", DependsOn: []string{"a"}, Condition: `status == "rejected"`},
		{ID: "d", DependsOn: []string{"b", "c"}},
	}

	graph, err := NewGraph(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	env := map[string]any{"status": "approved"}
	if !mustEvaluate(t, graph, "b", env) {
		t.Fatal("expected branch b to match")
	}
	if mustEvaluate(t, graph, "c", env) {
		t.Fatal("expected branch c to skip")
	}

	graph.MarkComplete("a")
	graph.MarkComplete("b")
	ready := graph.SkipStep("c")
	if len(ready) != 1 || ready[0] != "d" {
		t.Fatalf("expected d ready after skip, got %v", ready)
	}
}

func TestGraph_SkipPropagation(t *testing.T) {
	steps := []types.Step{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "d", DependsOn: []string{"a", "b"}},
	}

	graph, err := NewGraph(steps)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	graph.MarkComplete("a")
	ready := graph.SkipStep("b")
	if len(ready) != 1 || ready[0] != "d" {
		t.Fatalf("expected d ready after skipping b, got %v", ready)
	}
}

func TestGraph_EmptyGraph(t *testing.T) {
	graph, err := NewGraph(nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready := graph.ReadySteps(); len(ready) != 0 {
		t.Fatalf("expected empty ready list, got %v", ready)
	}
}

func TestGraph_SingleNode(t *testing.T) {
	graph, err := NewGraph([]types.Step{{ID: "solo"}})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if ready := graph.ReadySteps(); len(ready) != 1 || ready[0] != "solo" {
		t.Fatalf("expected solo ready, got %v", ready)
	}
}

func TestGraph_IsolatedNode(t *testing.T) {
	graph, err := NewGraph([]types.Step{
		{ID: "a"},
		{ID: "b", DependsOn: []string{"a"}},
		{ID: "isolated"},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	ready := graph.ReadySteps()
	if len(ready) != 2 {
		t.Fatalf("expected two ready nodes, got %v", ready)
	}
}

func mustEvaluate(t *testing.T, graph *Graph, stepID string, env map[string]any) bool {
	t.Helper()
	matched, err := graph.EvaluateCondition(stepID, env)
	if err != nil {
		t.Fatalf("evaluate condition failed: %v", err)
	}
	return matched
}
