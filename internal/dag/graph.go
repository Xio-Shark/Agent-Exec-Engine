package dag

import (
	"fmt"
	"sync"

	"github.com/google/cel-go/cel"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

// Graph represents a directed acyclic graph of workflow steps.
type Graph struct {
	mu       sync.RWMutex
	steps    map[string]*types.Step
	edges    map[string][]string
	inDegree map[string]int
	resolved map[string]types.StepStatus
}

// NewGraph builds a DAG from a workflow definition.
func NewGraph(steps []types.Step) (*Graph, error) {
	g := &Graph{
		steps:    make(map[string]*types.Step, len(steps)),
		edges:    make(map[string][]string),
		inDegree: make(map[string]int, len(steps)),
		resolved: make(map[string]types.StepStatus),
	}
	if err := g.indexSteps(steps); err != nil {
		return nil, err
	}
	if err := g.buildEdges(steps); err != nil {
		return nil, err
	}
	if err := g.validateAcyclic(); err != nil {
		return nil, err
	}
	return g, nil
}

// ReadySteps returns unresolved steps whose upstream dependencies are satisfied.
func (g *Graph) ReadySteps() []string {
	g.mu.RLock()
	defer g.mu.RUnlock()

	ready := make([]string, 0, len(g.inDegree))
	for id, deg := range g.inDegree {
		if deg == 0 && !g.isResolvedLocked(id) {
			ready = append(ready, id)
		}
	}
	return ready
}

// MarkComplete marks a step as successfully resolved.
func (g *Graph) MarkComplete(stepID string) []string {
	return g.resolveStep(stepID, types.StepSuccess)
}

// SkipStep marks a step as skipped while still unblocking its dependents.
func (g *Graph) SkipStep(stepID string) []string {
	return g.resolveStep(stepID, types.StepSkipped)
}

// EvaluateCondition compiles and evaluates a CEL expression for the step.
func (g *Graph) EvaluateCondition(stepID string, env map[string]any) (bool, error) {
	step, ok := g.Step(stepID)
	if !ok {
		return false, fmt.Errorf("step %q not found", stepID)
	}
	if step.Condition == "" {
		return true, nil
	}

	condEnv, vars := buildConditionEnv(env)
	celEnv, err := cel.NewEnv(vars...)
	if err != nil {
		return false, fmt.Errorf("create CEL env for step %q: %w", stepID, err)
	}

	ast, iss := celEnv.Compile(step.Condition)
	if iss != nil && iss.Err() != nil {
		return false, fmt.Errorf("compile condition for step %q: %w", stepID, iss.Err())
	}

	program, err := celEnv.Program(ast)
	if err != nil {
		return false, fmt.Errorf("build CEL program for step %q: %w", stepID, err)
	}

	out, _, err := program.Eval(condEnv)
	if err != nil {
		return false, fmt.Errorf("evaluate condition for step %q: %w", stepID, err)
	}

	matched, ok := out.Value().(bool)
	if !ok {
		return false, fmt.Errorf("condition for step %q returned %T, want bool", stepID, out.Value())
	}
	return matched, nil
}

// Step returns the step definition by ID.
func (g *Graph) Step(id string) (*types.Step, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	step, ok := g.steps[id]
	return step, ok
}

// StepCount returns the total number of steps.
func (g *Graph) StepCount() int {
	g.mu.RLock()
	defer g.mu.RUnlock()
	return len(g.steps)
}

func (g *Graph) indexSteps(steps []types.Step) error {
	for i := range steps {
		step := &steps[i]
		if _, exists := g.steps[step.ID]; exists {
			return fmt.Errorf("duplicate step ID: %s", step.ID)
		}
		g.steps[step.ID] = step
		g.inDegree[step.ID] = 0
	}
	return nil
}

func (g *Graph) buildEdges(steps []types.Step) error {
	for _, step := range steps {
		for _, depID := range step.DependsOn {
			if _, exists := g.steps[depID]; !exists {
				return fmt.Errorf("step %q depends on unknown step %q", step.ID, depID)
			}
			g.edges[depID] = append(g.edges[depID], step.ID)
			g.inDegree[step.ID]++
		}
	}
	return nil
}

func (g *Graph) resolveStep(stepID string, status types.StepStatus) []string {
	g.mu.Lock()
	defer g.mu.Unlock()

	if _, exists := g.steps[stepID]; !exists || g.isResolvedLocked(stepID) {
		return nil
	}

	g.resolved[stepID] = status
	delete(g.inDegree, stepID)

	newlyReady := make([]string, 0, len(g.edges[stepID]))
	for _, downstream := range g.edges[stepID] {
		if g.isResolvedLocked(downstream) {
			continue
		}
		deg, ok := g.inDegree[downstream]
		if !ok {
			continue
		}
		g.inDegree[downstream] = deg - 1
		if g.inDegree[downstream] == 0 {
			newlyReady = append(newlyReady, downstream)
		}
	}
	return newlyReady
}

func (g *Graph) isResolvedLocked(stepID string) bool {
	_, ok := g.resolved[stepID]
	return ok
}

func buildConditionEnv(env map[string]any) (map[string]any, []cel.EnvOption) {
	if env == nil {
		env = map[string]any{}
	}

	vars := make([]cel.EnvOption, 0, len(env))
	for key := range env {
		vars = append(vars, cel.Variable(key, cel.DynType))
	}
	return env, vars
}

// validateAcyclic uses Kahn's algorithm to check for cycles.
func (g *Graph) validateAcyclic() error {
	inDeg := make(map[string]int, len(g.inDegree))
	for key, value := range g.inDegree {
		inDeg[key] = value
	}

	queue := make([]string, 0, len(inDeg))
	for id, deg := range inDeg {
		if deg == 0 {
			queue = append(queue, id)
		}
	}

	visited := 0
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		visited++
		for _, downstream := range g.edges[current] {
			inDeg[downstream]--
			if inDeg[downstream] == 0 {
				queue = append(queue, downstream)
			}
		}
	}

	if visited != len(g.steps) {
		return fmt.Errorf("workflow graph contains a cycle (%d/%d steps reachable)", visited, len(g.steps))
	}
	return nil
}
