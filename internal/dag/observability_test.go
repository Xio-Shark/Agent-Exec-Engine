package dag

import (
	"context"
	"errors"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"

	"github.com/Xio-Shark/agent-exec-engine/internal/observability"
	"github.com/Xio-Shark/agent-exec-engine/internal/store"
	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestScheduler_RecordsMetrics(t *testing.T) {
	promRegistry := prometheus.NewRegistry()
	metrics := observability.NewMetricsWithRegisterer(promRegistry)
	executor := &mockExecutor{
		outputs: map[string]string{
			"a": `{"ok":true}`,
			"b": `{"done":true}`,
		},
		errors: map[string][]error{
			"b": {errors.New("retry me")},
		},
	}
	scheduler, err := NewScheduler(&types.Workflow{
		ID: "wf",
		Steps: []types.Step{
			{ID: "a", Type: types.StepTypeLLMCall},
			{ID: "b", Type: types.StepTypeLLMCall, DependsOn: []string{"a"}, Retry: &types.RetryPolicy{MaxRetries: 1}},
		},
	}, map[types.StepType]StepExecutor{
		types.StepTypeLLMCall: executor,
	}, WithMetrics(metrics))
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	run, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	if run.Status != types.WorkflowCompleted {
		t.Fatalf("expected completed workflow, got %s", run.Status)
	}

	if got := counterValue(t, metrics.WorkflowsTotal.WithLabelValues("completed")); got != 1 {
		t.Fatalf("expected completed workflows 1, got %v", got)
	}
	if got := counterValue(t, metrics.StepRetries.WithLabelValues("b")); got != 1 {
		t.Fatalf("expected retry count 1, got %v", got)
	}
	if got := histogramCountByName(t, promRegistry, "agent_exec_step_duration_seconds", map[string]string{
		"step_type": string(types.StepTypeLLMCall),
		"status":    string(types.StepSuccess),
	}); got != 2 {
		t.Fatalf("expected two successful step observations, got %d", got)
	}
	if got := histogramCountByName(t, promRegistry, "agent_exec_step_duration_seconds", map[string]string{
		"step_type": string(types.StepTypeLLMCall),
		"status":    string(types.StepFailed),
	}); got != 1 {
		t.Fatalf("expected one failed observation, got %d", got)
	}
}

func TestScheduler_RecordsCheckpointMetrics(t *testing.T) {
	metrics := observability.NewMetricsWithRegisterer(prometheus.NewRegistry())
	mem := store.NewMemoryStore()
	checkpointer := NewRedisCheckpointer(mem)

	scheduler, err := NewScheduler(&types.Workflow{
		ID:    "wf",
		Steps: []types.Step{{ID: "a", Type: types.StepTypeLLMCall}},
	}, map[types.StepType]StepExecutor{
		types.StepTypeLLMCall: &mockExecutor{output: `{"ok":true}`},
	}, WithCheckpointer(checkpointer), WithMetrics(metrics))
	if err != nil {
		t.Fatalf("new scheduler failed: %v", err)
	}

	run, err := scheduler.Run(context.Background())
	if err != nil {
		t.Fatalf("run failed: %v", err)
	}
	loaded, err := checkpointer.Load(context.Background(), run.ID)
	if err != nil {
		t.Fatalf("load checkpoint failed: %v", err)
	}

	restored, err := RestoreScheduler(context.Background(), &types.Workflow{
		ID:    "wf",
		Steps: []types.Step{{ID: "a", Type: types.StepTypeLLMCall}},
	}, loaded, map[types.StepType]StepExecutor{
		types.StepTypeLLMCall: &mockExecutor{output: `{"ok":true}`},
	}, WithMetrics(metrics))
	if err != nil {
		t.Fatalf("restore scheduler failed: %v", err)
	}
	if restored == nil {
		t.Fatal("expected restored scheduler")
	}

	if got := counterValue(t, metrics.CheckpointSaved); got != 1 {
		t.Fatalf("expected checkpoint saved count 1, got %v", got)
	}
	if got := counterValue(t, metrics.CheckpointLoaded); got != 1 {
		t.Fatalf("expected checkpoint loaded count 1, got %v", got)
	}
}

func counterValue(t *testing.T, collector interface{ Write(*dto.Metric) error }) float64 {
	t.Helper()

	metric := &dto.Metric{}
	if err := collector.Write(metric); err != nil {
		t.Fatalf("write metric: %v", err)
	}
	return metric.GetCounter().GetValue()
}

func histogramCountByName(
	t *testing.T,
	registry *prometheus.Registry,
	metricName string,
	labels map[string]string,
) uint64 {
	t.Helper()

	families, err := registry.Gather()
	if err != nil {
		t.Fatalf("gather metrics: %v", err)
	}
	for _, family := range families {
		if family.GetName() != metricName {
			continue
		}
		for _, metric := range family.GetMetric() {
			if hasLabels(metric, labels) {
				return metric.GetHistogram().GetSampleCount()
			}
		}
	}
	t.Fatalf("metric %s with labels %#v not found", metricName, labels)
	return 0
}

func hasLabels(metric *dto.Metric, labels map[string]string) bool {
	for key, expected := range labels {
		found := false
		for _, pair := range metric.GetLabel() {
			if pair.GetName() == key && pair.GetValue() == expected {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	return true
}
