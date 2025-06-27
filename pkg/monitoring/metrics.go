package monitoring

import (
	"net/http"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Package monitoring provides Prometheus metrics for FluxCache.
// Metrics: request count, error count, latency, node health.
// Use InitMetrics() in main, and InstrumentHandler to wrap endpoints.
// Expose /metrics for Prometheus scraping.
//
// Example Prometheus config and usage in README.
//
// Author: Syed Daniyal Hassan

var (
	RequestCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fluxcache_requests_total",
			Help: "Total number of requests by type.",
		},
		[]string{"method"},
	)
	ErrorCount = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "fluxcache_errors_total",
			Help: "Total number of errors by type.",
		},
		[]string{"method"},
	)
	RequestLatency = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "fluxcache_request_latency_seconds",
			Help:    "Request latency in seconds.",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method"},
	)
	NodeHealth = prometheus.NewGaugeVec(
		prometheus.GaugeOpts{
			Name: "fluxcache_node_health",
			Help: "Health status of nodes (1=healthy, 0=unhealthy).",
		},
		[]string{"node"},
	)
)

func InitMetrics() {
	prometheus.MustRegister(RequestCount, ErrorCount, RequestLatency, NodeHealth)
}

// InstrumentHandler wraps an http.HandlerFunc to collect metrics
func InstrumentHandler(method string, handler http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		RequestCount.WithLabelValues(method).Inc()
		rw := &responseWriter{ResponseWriter: w, status: 200}
		handler(rw, r)
		RequestLatency.WithLabelValues(method).Observe(time.Since(start).Seconds())
		if rw.status >= 400 {
			ErrorCount.WithLabelValues(method).Inc()
		}
	}
}

type responseWriter struct {
	http.ResponseWriter
	status int
}

func (rw *responseWriter) WriteHeader(code int) {
	rw.status = code
	rw.ResponseWriter.WriteHeader(code)
}

func PrometheusHandler() http.Handler {
	return promhttp.Handler()
}
