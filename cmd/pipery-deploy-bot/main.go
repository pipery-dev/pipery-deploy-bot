package main

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pipery-dev/pipery-deploy-bot/internal/config"
	"github.com/pipery-dev/pipery-deploy-bot/internal/github"
	"github.com/pipery-dev/pipery-deploy-bot/internal/httpapi"
	"github.com/pipery-dev/pipery-deploy-bot/internal/scheduler"
	"github.com/pipery-dev/pipery-deploy-bot/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("load config: %v", err)
	}

	db := sql.OpenDB(stdlib.GetConnector(*cfg.PGConfig))
	defer db.Close()
	if err := db.Ping(); err != nil {
		log.Fatalf("connect postgres: %v", err)
	}

	storage := store.NewPostgres(db)
	auth := github.NewAppAuthenticator(http.DefaultClient, cfg.Installations)
	gh := github.NewClient(http.DefaultClient, auth)
	sched := scheduler.New(storage, gh, cfg.SchedulerInterval, 5)
	server := httpapi.NewServer(storage, cfg.APIToken)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go sched.Run(ctx)

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr,
		Handler:           server.Routes(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      60 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("pipery-deploy-bot listening on %s", cfg.ListenAddr)
		errCh <- httpServer.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Printf("shutting down")
	case err := <-errCh:
		if !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("http server: %v", err)
		}
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}
