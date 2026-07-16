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
	"path/filepath"
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

	if *migrateOnly {
		db, err := config.LoadDatabase()
		if err != nil {
			log.Fatalf("config: %v", err)
		}
		if err := applyMigrations(db.DatabaseURL); err != nil {
			log.Fatalf("migrations: %v", err)
		}
		log.Println("migrations applied; exiting (migrate-only)")
		return
	}

	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	if cfg.AutoMigrate {
		if err := applyMigrations(cfg.DatabaseURL); err != nil {
			log.Fatalf("migrations: %v", err)
		}
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

	if err := migrateLegacyLocalStorage(cfg.Storage); err != nil {
		log.Fatalf("storage: %v", err)
	}
	storageSvc, err := newObjectStorage(ctx, cfg.Storage)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}

	srv := server.NewWithOptions(st, hub, server.Options{
		CORSAllowedOrigins: cfg.CORSAllowedOrigins,
		PublicOrigin:       cfg.PublicOrigin,
		TrustedProxyCIDRs:  cfg.TrustedProxyCIDRs,
		SessionTTL:         cfg.SessionTTL,
		DevReload:          cfg.DevReload,
		ObjectStorage:      storageSvc,
	})

	httpSrv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           srv.Router(),
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
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

func migrateLegacyLocalStorage(cfg config.StorageConfig) error {
	if cfg.Backend != "local" || cfg.LocalRoot != config.DefaultLocalStorageRoot {
		return nil
	}

	legacyRoot := config.LegacyLocalStorageRoot
	legacyInfo, err := os.Stat(legacyRoot)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("inspect legacy local storage root %q: %w", legacyRoot, err)
	}
	if !legacyInfo.IsDir() {
		return fmt.Errorf("legacy local storage root %q is not a directory", legacyRoot)
	}
	if _, err := os.Stat(cfg.LocalRoot); err == nil {
		return fmt.Errorf("cannot relocate legacy local storage from %q to %q: destination already exists; move the files manually", legacyRoot, cfg.LocalRoot)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("inspect local storage root %q: %w", cfg.LocalRoot, err)
	}
	if err := os.MkdirAll(filepath.Dir(cfg.LocalRoot), 0700); err != nil {
		return fmt.Errorf("create local storage parent directory: %w", err)
	}
	if err := os.Rename(legacyRoot, cfg.LocalRoot); err != nil {
		return fmt.Errorf("relocate legacy local storage from %q to %q: %w", legacyRoot, cfg.LocalRoot, err)
	}
	return nil
}

func newObjectStorage(ctx context.Context, cfg config.StorageConfig) (*objectstorage.Service, error) {
	switch cfg.Backend {
	case "local":
		return objectstorage.NewLocalService(cfg.LocalRoot, cfg.Bucket, cfg.MaxUploadBytes)
	case "s3":
		return objectstorage.NewS3Service(ctx, cfg.Bucket, cfg.MaxUploadBytes, objectstorage.S3Config{
			Endpoint:       cfg.S3Endpoint,
			Region:         cfg.S3Region,
			ForcePathStyle: cfg.S3ForcePathStyle,
		})
	default:
		return nil, fmt.Errorf("unsupported storage backend %q", cfg.Backend)
	}
}

func applyMigrations(dbURL string) error {
	db, err := sql.Open("pgx", dbURL)
	if err != nil {
		return err
	}
	defer db.Close()
	return migrations.Up(db)
}
