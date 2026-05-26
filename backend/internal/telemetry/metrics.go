package telemetry

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"os"
	"strings"
	"time"

	prometheusexporter "go.opentelemetry.io/otel/exporters/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// StartPrometheusMetrics starts a lightweight HTTP server on METRICS_ADDR
// (default :9090) that exposes /metrics in Prometheus text format.
// It registers a Prometheus reader with the provided MeterProvider, or creates
// a standalone MeterProvider when pipeline is nil or not enabled.
// The returned stop function shuts the metrics server down gracefully.
func StartPrometheusMetrics(ctx context.Context, res *resource.Resource) (stop func(), err error) {
	addr := strings.TrimSpace(os.Getenv("METRICS_ADDR"))
	if addr == "" {
		addr = ":9090"
	}

	exporter, err := prometheusexporter.New()
	if err != nil {
		return nil, err
	}

	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(exporter),
	)

	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.Handler())

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	ln, err := net.Listen("tcp", addr)
	if err != nil {
		_ = mp.Shutdown(ctx)
		return nil, err
	}

	go func() {
		slog.Info("metrics server listening", "addr", addr)
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			slog.Error("metrics server error", "error", err)
		}
	}()

	return func() {
		shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutCtx)
		_ = mp.Shutdown(shutCtx)
	}, nil
}

// PrometheusEnabled reports whether METRICS_ENABLED is set to a truthy value.
func PrometheusEnabled() bool {
	v := strings.ToLower(strings.TrimSpace(os.Getenv("METRICS_ENABLED")))
	return v == "1" || v == "true" || v == "yes"
}
