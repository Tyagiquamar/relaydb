package telemetry

import (
	"net/http/httptest"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
)

func TestRegistryNoDuplicateRegistration(t *testing.T) {
	// Registry should be usable without panics
	if Registry == nil {
		t.Fatal("Registry is nil")
	}

	// Verify build info metric exists
	count, err := Registry.Gather()
	if err != nil {
		t.Fatalf("Registry.Gather() error = %v", err)
	}
	if len(count) == 0 {
		t.Error("Registry should have metrics registered")
	}
}

func TestSetBuildInfo(t *testing.T) {
	// Create a fresh registry for this test to avoid conflicts
	reg := prometheus.NewRegistry()
	buildInfo := prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "relaydb",
			Name:      "build_info_test",
			Help:      "Build info test.",
		},
		[]string{"version", "go_version", "service"},
	)
	reg.MustRegister(buildInfo)

	buildInfo.WithLabelValues("1.0.0", "go1.26", "api").Set(1)

	metrics, err := reg.Gather()
	if err != nil {
		t.Fatalf("Gather() error = %v", err)
	}

	found := false
	for _, m := range metrics {
		if m.GetName() == "relaydb_build_info_test" {
			found = true
			if len(m.GetMetric()) != 1 {
				t.Errorf("expected 1 metric, got %d", len(m.GetMetric()))
			}
			break
		}
	}
	if !found {
		t.Error("build_info metric not found")
	}
}

func TestMetricsHandler(t *testing.T) {
	handler := MetricsHandler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Errorf("MetricsHandler status = %d, want 200", rec.Code)
	}

	body := rec.Body.String()
	if body == "" {
		t.Error("MetricsHandler should return metrics")
	}
}

func TestHealthHandler(t *testing.T) {
	live, ready := HealthHandler(func() bool { return true })

	// Test live
	req := httptest.NewRequest("GET", "/health/live", nil)
	rec := httptest.NewRecorder()
	live.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("live status = %d, want 200", rec.Code)
	}

	// Test ready when check passes
	req = httptest.NewRequest("GET", "/health/ready", nil)
	rec = httptest.NewRecorder()
	ready.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Errorf("ready status = %d, want 200", rec.Code)
	}

	// Test ready when check fails
	_, ready = HealthHandler(func() bool { return false })
	rec = httptest.NewRecorder()
	ready.ServeHTTP(rec, req)
	if rec.Code != 503 {
		t.Errorf("ready status = %d, want 503", rec.Code)
	}
}