package main

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/babelsuite/babelsuite/internal/envloader"
	"github.com/babelsuite/babelsuite/internal/telemetry"
)

func main() {
	envloader.Load(".env", "../.env")

	a, err := newApp(context.Background())
	if err != nil {
		log.Fatal(err)
	}

	telemetry.InitDefaultLogger(telemetry.NewLogger(nil))

	addr := envOr("PORT", "8090")
	if !strings.Contains(addr, ":") {
		addr = ":" + addr
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
