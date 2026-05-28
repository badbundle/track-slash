package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/bradleymackey/track-slash/internal/config"
	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/realtime"
	"github.com/bradleymackey/track-slash/internal/server"
	"github.com/bradleymackey/track-slash/internal/store"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "run migrations and exit")
	flag.Parse()

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if err := applyMigrations(cfg.DatabaseURL); err != nil {
		log.Fatalf("migrations: %v", err)
	}
	if *migrateOnly {
		log.Println("migrations applied; exiting (migrate-only)")
		return
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Fatalf("pgxpool: %v", err)
	}
	defer pool.Close()

	if err := pool.Ping(ctx); err != nil {
		log.Fatalf("pg ping: %v", err)
	}

	st := store.New(pool)

	hub := realtime.NewHub()
	listener := realtime.NewListener(cfg.DatabaseURL, hub)
	go listener.Run(ctx)

	srv := server.New(st, hub, cfg.CORSAllowedOrigins)

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
	}

	errCh := make(chan error, 1)
	go func() {
		log.Printf("listening on :%s", cfg.Port)
		errCh <- httpSrv.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		log.Println("shutdown signal received")
	case err := <-errCh:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("server: %v", err)
		}
	}

	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelShutdown()
	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
	log.Println("bye")
}

func applyMigrations(dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	return migrations.Up(db)
}
