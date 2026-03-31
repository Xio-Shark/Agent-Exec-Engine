package infra

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSchedulerClient_RequestGPU(t *testing.T) {
	t.Parallel()

	createCalled := false
	scheduleCalled := false
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jobs":
			createCalled = true
			var payload map[string]any
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Fatalf("decode request: %v", err)
			}
			resourceSpec := payload["resource_spec"].(map[string]any)
			if got := int(resourceSpec["gpu"].(float64)); got != 2 {
				t.Fatalf("gpu = %d, want 2", got)
			}
			if got := resourceSpec["gpu_memory"].(string); got != "8192Mi" {
				t.Fatalf("gpu_memory = %s, want 8192Mi", got)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"job-1"}`))
		case "/jobs/job-1/schedule":
			scheduleCalled = true
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"scheduled"}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewSchedulerClient(server.URL, time.Second)
	if err := client.RequestGPU(context.Background(), "step-a", 2, 8192); err != nil {
		t.Fatalf("RequestGPU() error = %v", err)
	}
	if !createCalled || !scheduleCalled {
		t.Fatalf("expected create and schedule to be called, got create=%v schedule=%v", createCalled, scheduleCalled)
	}
}

func TestSchedulerClient_ReleaseGPUUnsupported(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/jobs":
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id":"job-1"}`))
		case "/jobs/job-1/schedule":
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"status":"scheduled"}`))
		case "/jobs/job-1/release":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client := NewSchedulerClient(server.URL, time.Second)
	if err := client.RequestGPU(context.Background(), "step-a", 1, 0); err != nil {
		t.Fatalf("RequestGPU() error = %v", err)
	}
	err := client.ReleaseGPU(context.Background(), "step-a")
	if err != ErrReleaseUnsupported {
		t.Fatalf("ReleaseGPU() error = %v, want %v", err, ErrReleaseUnsupported)
	}
}
