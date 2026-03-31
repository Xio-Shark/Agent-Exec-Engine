package dag

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/Xio-Shark/agent-exec-engine/pkg/types"
)

func TestCanTransition_AllValid(t *testing.T) {
	for from, next := range ValidTransitions {
		for _, to := range next {
			if !CanTransition(from, to) {
				t.Fatalf("expected transition %s -> %s to be valid", from, to)
			}
		}
	}
}

func TestCanTransition_Invalid(t *testing.T) {
	if CanTransition(types.StepSuccess, types.StepRunning) {
		t.Fatal("success -> running should be invalid")
	}
}

func TestShouldRetry_NoPolicy(t *testing.T) {
	if ShouldRetry(&types.Step{}, &types.StepState{Status: types.StepFailed}) {
		t.Fatal("expected no retry without retry policy")
	}
}

func TestShouldRetry_ExhaustedRetries(t *testing.T) {
	step := &types.Step{Retry: &types.RetryPolicy{MaxRetries: 2}}
	state := &types.StepState{Status: types.StepFailed, RetryCount: 2}
	if ShouldRetry(step, state) {
		t.Fatal("expected retries to be exhausted")
	}
}

func TestStepState_JSONRoundTrip(t *testing.T) {
	now := time.Now()
	original := &types.StepState{
		StepID:      "a",
		Status:      types.StepSuccess,
		StartedAt:   &now,
		CompletedAt: &now,
		Output:      `{"ok":true}`,
		RetryCount:  1,
		TokensUsed:  42,
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var restored types.StepState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if restored.StepID != original.StepID || restored.Output != original.Output {
		t.Fatalf("round trip mismatch: %#v vs %#v", restored, original)
	}
}
