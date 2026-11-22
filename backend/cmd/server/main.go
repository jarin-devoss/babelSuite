package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/babelsuite/babelsuite/internal/envloader"
	"github.com/babelsuite/babelsuite/internal/telemetry"
	sdkresource "go.opentelemetry.io/otel/sdk/resource"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

func main() {
	envloader.Load(".env", "../.env")

	a, err := newApp(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	telemetry.InitDefaultLogger(telemetry.NewLogger(nil))

	if telemetry.PrometheusEnabled() {
		res, _ := sdkresource.New(context.Background(),
			sdkresource.WithTelemetrySDK(),
			sdkresource.WithAttributes(semconv.ServiceNameKey.String("babelsuite-backend")),
		)
		if stopMetrics, err := telemetry.StartPrometheusMetrics(context.Background(), res); err != nil {
			log.Printf("metrics: %v", err)
		} else {
			defer stopMetrics()
		}
	}

	addr := envOr("PORT", "8090")
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
	}
	// A suite's gateway proxies mocks back to this process from inside its
	// container, so it has to be told the port this one listens on.
	if os.Getenv("BABELSUITE_ENGINE_ADDR") == "" {
		if _, port, err := net.SplitHostPort(addr); err == nil && port != "" {
			_ = os.Setenv("BABELSUITE_ENGINE_ADDR", "host.docker.internal:"+port)
		}
	}

	server := &http.Server{
		Addr:              addr,
		Handler:           a,
		ReadHeaderTimeout: durationOr("HTTP_READ_HEADER_TIMEOUT", 5*time.Second),
		ReadTimeout:       durationOr("HTTP_READ_TIMEOUT", 30*time.Second),
		WriteTimeout:      durationOr("HTTP_WRITE_TIMEOUT", 2*time.Minute),
		IdleTimeout:       durationOr("HTTP_IDLE_TIMEOUT", 2*time.Minute),
		MaxHeaderBytes:    1 << 14,
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	defer signal.Stop(stop)

	go func() {
		<-stop
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			slog.Error("server shutdown", "error", err)
		}
	}()

	if os.Getenv("BABELSUITE_REAP_ON_START") != "false" {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			a.ReapLeftovers(ctx)
		}()
	}

	slog.Info("babelsuite server listening", "addr", addr)
	if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		slog.Error("server error", "error", err)
		os.Exit(1)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := a.Close(shutdownCtx); err != nil {
		slog.Error("control plane shutdown", "error", err)
	}
}
