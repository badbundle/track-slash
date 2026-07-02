package storage

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type LocalBackend struct {
	root string
}

func NewLocalBackend(root string) (*LocalBackend, error) {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil, errors.New("local storage root is required")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &LocalBackend{root: filepath.Clean(abs)}, nil
}

func (b *LocalBackend) Put(ctx context.Context, key string, r io.Reader, maxBytes int64) (WrittenObject, error) {
	if maxBytes <= 0 {
		return WrittenObject{}, errors.New("max bytes must be positive")
	}
	path, err := b.pathForKey(key)
	if err != nil {
		return WrittenObject{}, err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return WrittenObject{}, err
	}
	if _, err := os.Stat(path); err == nil {
		return WrittenObject{}, ErrExists
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return WrittenObject{}, err
	}

	tmp, err := os.CreateTemp(filepath.Dir(path), "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return WrittenObject{}, err
	}
	tmpName := tmp.Name()
	removeTmp := true
	defer func() {
		if removeTmp {
			_ = os.Remove(tmpName)
		}
	}()

	hasher := sha256.New()
	counter := &countingWriter{w: io.MultiWriter(tmp, hasher)}
	limited := io.LimitReader(&contextReader{ctx: ctx, r: r}, maxBytes+1)
	if _, err := io.Copy(counter, limited); err != nil {
		_ = tmp.Close()
		return WrittenObject{}, err
	}
	if counter.n > maxBytes {
		_ = tmp.Close()
		return WrittenObject{}, ErrTooLarge
	}
	if err := tmp.Close(); err != nil {
		return WrittenObject{}, err
	}
	if err := os.Rename(tmpName, path); err != nil {
		if errors.Is(err, os.ErrExist) {
			return WrittenObject{}, ErrExists
		}
		return WrittenObject{}, err
	}
	removeTmp = false

	return WrittenObject{
		Size:   counter.n,
		SHA256: hexHash(hasher),
	}, nil
}

func (b *LocalBackend) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	path, err := b.pathForKey(key)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return f, nil
}

func (b *LocalBackend) Delete(ctx context.Context, key string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	path, err := b.pathForKey(key)
	if err != nil {
		return err
	}
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	return nil
}

func (b *LocalBackend) pathForKey(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" || strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return "", ErrInvalidKey
	}
	cleanKey := filepath.Clean(filepath.FromSlash(key))
	if cleanKey == "." || strings.HasPrefix(cleanKey, ".."+string(filepath.Separator)) || cleanKey == ".." {
		return "", ErrInvalidKey
	}
	path := filepath.Join(b.root, cleanKey)
	rel, err := filepath.Rel(b.root, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", ErrInvalidKey
	}
	return path, nil
}

func hexHash(h hash.Hash) string {
	return hex.EncodeToString(h.Sum(nil))
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (w *countingWriter) Write(p []byte) (int, error) {
	n, err := w.w.Write(p)
	w.n += int64(n)
	return n, err
}

type contextReader struct {
	ctx context.Context
	r   io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.r.Read(p)
	if err != nil && !errors.Is(err, io.EOF) {
		return n, fmt.Errorf("read storage source: %w", err)
	}
	return n, err
}
