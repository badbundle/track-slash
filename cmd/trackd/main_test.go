package main

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/bradleymackey/track-slash/internal/config"
	objectstorage "github.com/bradleymackey/track-slash/internal/storage"
)

func TestMigrateLegacyLocalStorage(t *testing.T) {
	root := t.TempDir()
	changeWorkingDirectory(t, root)

	cfg := config.StorageConfig{Backend: "local", LocalRoot: config.DefaultLocalStorageRoot}
	if err := migrateLegacyLocalStorage(cfg); err != nil {
		t.Fatalf("migrateLegacyLocalStorage: %v", err)
	}
	if _, err := os.Stat(config.DefaultLocalStorageRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("default root stat err = %v, want not exist", err)
	}
}

func TestMigrateLegacyLocalStorageMovesObjects(t *testing.T) {
	root := t.TempDir()
	changeWorkingDirectory(t, root)

	key := "projects/p1/objects/o1"
	legacyPath := filepath.Join(config.LegacyLocalStorageRoot, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0700); err != nil {
		t.Fatalf("MkdirAll legacy storage: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("hello"), 0600); err != nil {
		t.Fatalf("WriteFile legacy object: %v", err)
	}

	cfg := config.StorageConfig{Backend: "local", LocalRoot: config.DefaultLocalStorageRoot}
	if err := migrateLegacyLocalStorage(cfg); err != nil {
		t.Fatalf("migrateLegacyLocalStorage: %v", err)
	}
	if _, err := os.Stat(config.LegacyLocalStorageRoot); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy root stat err = %v, want not exist", err)
	}

	backend, err := objectstorage.NewLocalBackend(config.DefaultLocalStorageRoot)
	if err != nil {
		t.Fatalf("NewLocalBackend: %v", err)
	}
	rc, err := backend.Open(context.Background(), key)
	if err != nil {
		t.Fatalf("Open migrated object: %v", err)
	}
	body, err := io.ReadAll(rc)
	if err != nil {
		t.Fatalf("ReadAll migrated object: %v", err)
	}
	if err := rc.Close(); err != nil {
		t.Fatalf("Close migrated object: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("migrated object = %q, want hello", body)
	}
}

func TestMigrateLegacyLocalStorageLeavesNonDefaultRootsUntouched(t *testing.T) {
	root := t.TempDir()
	changeWorkingDirectory(t, root)

	legacyPath := filepath.Join(config.LegacyLocalStorageRoot, "object")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0700); err != nil {
		t.Fatalf("MkdirAll legacy storage: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("hello"), 0600); err != nil {
		t.Fatalf("WriteFile legacy object: %v", err)
	}

	for _, cfg := range []config.StorageConfig{
		{Backend: "local", LocalRoot: "custom/storage"},
		{Backend: "s3", LocalRoot: config.DefaultLocalStorageRoot},
	} {
		if err := migrateLegacyLocalStorage(cfg); err != nil {
			t.Fatalf("migrateLegacyLocalStorage(%+v): %v", cfg, err)
		}
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy object stat: %v", err)
	}
}

func TestMigrateLegacyLocalStorageRejectsExistingDestination(t *testing.T) {
	root := t.TempDir()
	changeWorkingDirectory(t, root)

	legacyPath := filepath.Join(config.LegacyLocalStorageRoot, "legacy-object")
	if err := os.MkdirAll(filepath.Dir(legacyPath), 0700); err != nil {
		t.Fatalf("MkdirAll legacy storage: %v", err)
	}
	if err := os.WriteFile(legacyPath, []byte("legacy"), 0600); err != nil {
		t.Fatalf("WriteFile legacy object: %v", err)
	}
	destinationPath := filepath.Join(config.DefaultLocalStorageRoot, "new-object")
	if err := os.MkdirAll(filepath.Dir(destinationPath), 0700); err != nil {
		t.Fatalf("MkdirAll destination storage: %v", err)
	}
	if err := os.WriteFile(destinationPath, []byte("new"), 0600); err != nil {
		t.Fatalf("WriteFile destination object: %v", err)
	}

	cfg := config.StorageConfig{Backend: "local", LocalRoot: config.DefaultLocalStorageRoot}
	err := migrateLegacyLocalStorage(cfg)
	if err == nil || !strings.Contains(err.Error(), "destination already exists") {
		t.Fatalf("migrateLegacyLocalStorage err = %v, want destination error", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("legacy object stat: %v", err)
	}
	if _, err := os.Stat(destinationPath); err != nil {
		t.Fatalf("destination object stat: %v", err)
	}
}

func TestMigrateLegacyLocalStorageRejectsLegacyFile(t *testing.T) {
	root := t.TempDir()
	changeWorkingDirectory(t, root)

	if err := os.MkdirAll(filepath.Dir(config.LegacyLocalStorageRoot), 0700); err != nil {
		t.Fatalf("MkdirAll legacy parent: %v", err)
	}
	if err := os.WriteFile(config.LegacyLocalStorageRoot, []byte("not a directory"), 0600); err != nil {
		t.Fatalf("WriteFile legacy root: %v", err)
	}

	cfg := config.StorageConfig{Backend: "local", LocalRoot: config.DefaultLocalStorageRoot}
	err := migrateLegacyLocalStorage(cfg)
	if err == nil || !strings.Contains(err.Error(), "not a directory") {
		t.Fatalf("migrateLegacyLocalStorage err = %v, want directory error", err)
	}
}

func TestMigrateLegacyLocalStorageReturnsFilesystemErrors(t *testing.T) {
	t.Run("legacy root inspection", func(t *testing.T) {
		root := t.TempDir()
		changeWorkingDirectory(t, root)
		if err := os.WriteFile("tmp", []byte("not a directory"), 0600); err != nil {
			t.Fatalf("WriteFile tmp: %v", err)
		}

		err := migrateLegacyLocalStorage(defaultLocalStorageConfig())
		if err == nil || !strings.Contains(err.Error(), "inspect legacy local storage root") {
			t.Fatalf("migrateLegacyLocalStorage err = %v, want legacy inspection error", err)
		}
	})

	t.Run("destination root inspection", func(t *testing.T) {
		root := t.TempDir()
		changeWorkingDirectory(t, root)
		createLegacyStorageRoot(t)
		if err := os.WriteFile("data", []byte("not a directory"), 0600); err != nil {
			t.Fatalf("WriteFile data: %v", err)
		}

		err := migrateLegacyLocalStorage(defaultLocalStorageConfig())
		if err == nil || !strings.Contains(err.Error(), "inspect local storage root") {
			t.Fatalf("migrateLegacyLocalStorage err = %v, want destination inspection error", err)
		}
	})

	t.Run("destination parent creation", func(t *testing.T) {
		root := t.TempDir()
		changeWorkingDirectory(t, root)
		createLegacyStorageRoot(t)
		if err := os.Chmod(root, 0500); err != nil {
			t.Fatalf("Chmod root read-only: %v", err)
		}
		t.Cleanup(func() {
			if err := os.Chmod(root, 0700); err != nil {
				t.Fatalf("restore root permissions: %v", err)
			}
		})

		err := migrateLegacyLocalStorage(defaultLocalStorageConfig())
		if err == nil || !strings.Contains(err.Error(), "create local storage parent directory") {
			t.Fatalf("migrateLegacyLocalStorage err = %v, want parent creation error", err)
		}
	})

	t.Run("rename", func(t *testing.T) {
		root := t.TempDir()
		changeWorkingDirectory(t, root)
		createLegacyStorageRoot(t)
		if err := os.Mkdir("data", 0500); err != nil {
			t.Fatalf("Mkdir data: %v", err)
		}
		t.Cleanup(func() {
			if err := os.Chmod("data", 0700); err != nil {
				t.Fatalf("restore data permissions: %v", err)
			}
		})

		err := migrateLegacyLocalStorage(defaultLocalStorageConfig())
		if err == nil || !strings.Contains(err.Error(), "relocate legacy local storage") {
			t.Fatalf("migrateLegacyLocalStorage err = %v, want rename error", err)
		}
	})
}

func defaultLocalStorageConfig() config.StorageConfig {
	return config.StorageConfig{Backend: "local", LocalRoot: config.DefaultLocalStorageRoot}
}

func createLegacyStorageRoot(t *testing.T) {
	t.Helper()
	if err := os.MkdirAll(config.LegacyLocalStorageRoot, 0700); err != nil {
		t.Fatalf("MkdirAll legacy storage: %v", err)
	}
}

func changeWorkingDirectory(t *testing.T, dir string) {
	t.Helper()
	previous, err := os.Getwd()
	if err != nil {
		t.Fatalf("Getwd: %v", err)
	}
	if err := os.Chdir(dir); err != nil {
		t.Fatalf("Chdir(%q): %v", dir, err)
	}
	t.Cleanup(func() {
		if err := os.Chdir(previous); err != nil {
			t.Fatalf("restore working directory: %v", err)
		}
	})
}
