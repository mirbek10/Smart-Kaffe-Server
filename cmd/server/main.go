package main

import (
	"context"
	"errors"
	"flag"
	"koffe/api/internal/config"
	"koffe/api/internal/database"
	"koffe/api/internal/server"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"
)

func main() {
	seed := flag.Bool("seed", false, "Insert missing demo data before starting")
	flag.Parse()
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, nil)))
	c, e := config.Load()
	if e != nil {
		slog.Error("configuration invalid", "error", e)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	startup, cancel := context.WithTimeout(ctx, 45*time.Second)
	db, e := database.Open(startup, c)
	if e != nil {
		cancel()
		slog.Error("database connection failed", "error", e)
		os.Exit(1)
	}
	if e = database.Migrate(startup, db); e != nil {
		cancel()
		slog.Error("database migration failed")
		os.Exit(1)
	}
	if *seed {
		if c.Env == "production" {
			slog.Error("demo seed is disabled in production")
			os.Exit(1)
		}
		if e = database.Seed(startup, db); e != nil {
			slog.Error("seed failed")
			os.Exit(1)
		}
		slog.Warn("demo accounts initialized; change all default passwords")
	}
	cancel()
	app := server.New(db, c)
	httpServer := &http.Server{Addr: ":" + c.Port, Handler: app.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 30 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second, MaxHeaderBytes: 16 * 1024}
	go app.RunJobs(ctx)
	go func() {
		slog.Info("API listening", "port", c.Port, "swagger", "http://localhost:"+c.Port+"/swagger")
		if e := httpServer.ListenAndServe(); e != nil && !errors.Is(e, http.ErrServerClosed) {
			slog.Error("HTTP server failed")
			stop()
		}
	}()
	<-ctx.Done()
	shutdown, done := context.WithTimeout(context.Background(), 10*time.Second)
	defer done()
	_ = httpServer.Shutdown(shutdown)
	_ = db.Client().Disconnect(shutdown)
}
