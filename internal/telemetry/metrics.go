package telemetry

import (
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"runtime"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/collectors"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// BuildInfo exposes build/version information.
	BuildInfo = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Namespace: "relaydb",
			Name:      "build_info",
			Help:      "Build and version information.",
		},
		[]string{"version", "go_version", "service"},
	)

	// HTTPRequestsTotal counts HTTP requests by path and status.
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Namespace: "relaydb",
			Name:      "http_requests_total",
			Help:      "HTTP requests by path and status code.",
		},
		[]string{"path", "method", "status"},
	)

	// HTTPRequestDuration measures HTTP request latency.
	HTTPRequestDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Namespace: "relaydb",
			Name:      "http_request_duration_seconds",
			Help:      "HTTP request latency in seconds.",
			Buckets:   prometheus.DefBuckets,
		},
		[]string{"path", "method"},
	)
)

// Registry is the custom Prometheus registry for RelayDB.
var Registry = prometheus.NewRegistry()

func init() {
	Registry.MustRegister(
		BuildInfo,
		HTTPRequestsTotal,
		HTTPRequestDuration,
		collectors.NewGoCollector(),
		collectors.NewProcessCollector(collectors.ProcessCollectorOpts{}),
	)
}

// SetBuildInfo records build information.
func SetBuildInfo(version, service string) {
	BuildInfo.WithLabelValues(version, runtime.Version(), service).Set(1)
}

// MetricsHandler returns the Prometheus metrics HTTP handler.
func MetricsHandler() http.Handler {
	return promhttp.HandlerFor(Registry, promhttp.HandlerOpts{
		EnableOpenMetrics: true,
	})
}

// Logger returns the configured slog logger.
func Logger() *slog.Logger {
	level := slog.LevelInfo
	if os.Getenv("RELAYDB_DEBUG") == "true" {
		level = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
}

// HealthHandler returns handlers for liveness and readiness probes.
func HealthHandler(checkReady func() bool) (live, ready http.Handler) {
	live = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, `{"status":"alive"}`)
	})
	ready = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if checkReady == nil || !checkReady() {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = fmt.Fprintln(w, `{"status":"not ready"}`)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, `{"status":"ready"}`)
	})
	return live, ready
}
