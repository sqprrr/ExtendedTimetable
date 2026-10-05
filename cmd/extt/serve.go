package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/sqprrr/ExtendedTimetable/internal/server"
)

func cmdServe(ctx context.Context, args []string) error {
	fs := newFlagSet("serve")
	dbPath := dbFlag(fs)
	addr := fs.String("addr", envOr("EXTT_ADDR", "127.0.0.1:8080"), "listen address")
	secure := fs.Bool("secure-cookies", envBool("EXTT_SECURE_COOKIES", true), "mark cookies Secure (disable for local HTTP)")
	trustProxy := fs.Bool("trust-proxy", envBool("EXTT_TRUST_PROXY", false), "take client IP from X-Real-IP")
	if _, err := parseArgs(fs, args, 0); err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()

	st, err := openStore(ctx, *dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	svc := newService(st)
	handler, err := server.New(svc, server.Config{SecureCookies: *secure, TrustProxy: *trustProxy})
	if err != nil {
		return err
	}

	go func() {
		if err := svc.RunMaintenance(ctx, time.Hour); err != nil {
			slog.Error("maintenance stopped", "err", err)
		}
	}()

	srv := &http.Server{
		Addr:              *addr,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    64 << 10,
	}
	errc := make(chan error, 1)
	go func() {
		slog.Info("listening", "addr", *addr, "db", *dbPath, "secure_cookies", *secure)
		errc <- srv.ListenAndServe()
	}()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
	}
	slog.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
