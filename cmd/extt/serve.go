package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
	_ "time/tzdata" // the static binary must not depend on the host's zoneinfo

	"github.com/sqprrr/ExtendedTimetable/internal/metrics"
	"github.com/sqprrr/ExtendedTimetable/internal/server"
)

func cmdServe(ctx context.Context, args []string) error {
	fs := newFlagSet("serve")
	dbPath := dbFlag(fs)
	addr := fs.String("addr", envOr("EXTT_ADDR", "127.0.0.1:8080"), "listen address")
	secure := fs.Bool("secure-cookies", envBool("EXTT_SECURE_COOKIES", true), "mark cookies Secure (disable for local HTTP)")
	trustProxy := fs.Bool("trust-proxy", envBool("EXTT_TRUST_PROXY", false), "take client IP from X-Real-IP")
	tz := fs.String("tz", envOr("EXTT_TZ", "Europe/Kyiv"), "time zone for showing and entering dates")
	cistEvery := fs.Duration("cist-interval", envDuration("EXTT_CIST_INTERVAL", 6*time.Hour), "how often to sync schedules from CIST (0 turns it off)")
	baseURL := baseURLFlag(fs)
	metricsAddr := fs.String("metrics-addr", envOr("EXTT_METRICS_ADDR", ""), "listen address for Prometheus metrics at /metrics; keep it on loopback (empty turns it off)")
	logLevel := fs.String("log-level", envOr("EXTT_LOG_LEVEL", "info"), "debug, info, warn or error")
	logFormat := fs.String("log-format", envOr("EXTT_LOG_FORMAT", "text"), "text or json")
	if _, err := parseArgs(fs, args, 0); err != nil {
		return err
	}
	if err := setupLogging(*logLevel, *logFormat); err != nil {
		return err
	}
	loc, err := time.LoadLocation(*tz)
	if err != nil {
		return fmt.Errorf("--tz: %w", err)
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := openStore(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	svc := newService(st, loc)
	handler, err := server.New(svc, server.Config{SecureCookies: *secure, TrustProxy: *trustProxy, Location: loc, BaseURL: *baseURL})
	if err != nil {
		return err
	}

	go func() {
		if err := svc.RunMaintenance(ctx, time.Hour); err != nil {
			slog.Error("maintenance stopped", "err", err)
		}
	}()
	if *cistEvery > 0 {
		go svc.RunScheduleSync(ctx, *cistEvery)
	}

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	errc := make(chan error, 2)
	go func() {
		slog.Info("listening", "addr", *addr, "db", *dbPath, "secure_cookies", *secure, "trust_proxy", *trustProxy,
			"tz", loc.String(), "cist_interval", cistEvery.String(), "log_level", *logLevel)
		errc <- srv.ListenAndServe()
	}()

	// Metrics get a listener of their own so that nginx, which proxies only
	// --addr, never makes them public.
	var metricsSrv *http.Server
	if *metricsAddr != "" {
		metrics.RegisterStore(st, svc.Now)
		mux := http.NewServeMux()
		mux.Handle("GET /metrics", metrics.Handler())
		metricsSrv = &http.Server{
			Addr:              *metricsAddr,
			Handler:           mux,
			ReadHeaderTimeout: 10 * time.Second,
			WriteTimeout:      30 * time.Second,
		}
		go func() {
			slog.Info("serving metrics", "addr", *metricsAddr)
			if err := metricsSrv.ListenAndServe(); err != nil {
				errc <- fmt.Errorf("metrics: %w", err)
			}
		}()
	}

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if metricsSrv != nil {
		metricsSrv.Shutdown(shutdownCtx)
	}
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
