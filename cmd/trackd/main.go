package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	_ "github.com/jackc/pgx/v5/stdlib"

	"github.com/bradleymackey/track-slash/internal/config"
	"github.com/bradleymackey/track-slash/internal/migrations"
	"github.com/bradleymackey/track-slash/internal/model"
	"github.com/bradleymackey/track-slash/internal/realtime"
	"github.com/bradleymackey/track-slash/internal/server"
	objectstorage "github.com/bradleymackey/track-slash/internal/storage"
	"github.com/bradleymackey/track-slash/internal/store"
)

func main() {
	migrateOnly := flag.Bool("migrate-only", false, "run migrations and exit")
	createAdmin := flag.Bool("create-admin-token", false, "create or update an admin user, create an API token, print it, and exit")
	adminEmail := flag.String("email", "", "email for -create-admin-token")
	adminName := flag.String("name", "", "name for -create-admin-token")
	adminTokenName := flag.String("token-name", "bootstrap", "token name for -create-admin-token")
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
	if *createAdmin {
		if err := createAdminToken(ctx, st, *adminEmail, *adminName, *adminTokenName); err != nil {
			log.Fatalf("create admin token: %v", err)
		}
		return
	}

	hub := realtime.NewHub()
	listener := realtime.NewListener(cfg.DatabaseURL, hub)
	go listener.Run(ctx)

	storageSvc, err := objectstorage.NewLocalService(cfg.Storage.LocalRoot, cfg.Storage.Bucket, cfg.Storage.MaxUploadBytes)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	srv := server.NewWithOptions(st, hub, server.Options{
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		DevReload:          cfg.DevReload,
		MCPEnabled:         cfg.MCPEnabled,
		ObjectStorage:      storageSvc,
	})

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

func createAdminToken(ctx context.Context, st *store.Store, email, name, tokenName string) error {
	email = strings.TrimSpace(email)
	name = strings.TrimSpace(name)
	tokenName = strings.TrimSpace(tokenName)
	if email == "" || !strings.Contains(email, "@") {
		return errors.New("valid -email is required with -create-admin-token")
	}
	if name == "" {
		return errors.New("-name is required with -create-admin-token")
	}
	if tokenName == "" {
		return errors.New("-token-name is required with -create-admin-token")
	}
	u, err := st.CreateOrUpdateAdminUser(ctx, email, name)
	if err != nil {
		return err
	}
	created, err := st.CreateAuthToken(ctx, store.CreateAuthTokenParams{
		UserID: u.ID,
		Kind:   model.AuthTokenKindAPI,
		Name:   tokenName,
	})
	if err != nil {
		return err
	}
	fmt.Printf("user_id=%s\n", u.ID)
	fmt.Printf("token_id=%s\n", created.Token.ID)
	fmt.Printf("token=%s\n", created.RawToken)
	return nil
}

func applyMigrations(dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	return migrations.Up(db)
}
