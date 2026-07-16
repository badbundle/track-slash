package storage

import (
	"context"
	"errors"
	"fmt"
	"io"

	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("storage object not found")
	ErrTooLarge   = errors.New("storage object too large")
	ErrExists     = errors.New("storage object already exists")
	ErrInvalidKey = errors.New("invalid storage object key")
)

type WrittenObject struct {
	Size   int64
	SHA256 string
}

type Backend interface {
	Put(ctx context.Context, key string, r io.Reader, maxBytes int64) (WrittenObject, error)
	Open(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

type Service struct {
	backendName    string
	bucket         string
	maxUploadBytes int64
	backend        Backend
}

type StoredObject struct {
	Backend   string
	Bucket    string
	ObjectKey string
	ByteSize  int64
	SHA256    string
}

func NewService(backendName, bucket string, maxUploadBytes int64, backend Backend) (*Service, error) {
	if backendName == "" {
		return nil, errors.New("storage backend name is required")
	}
	if bucket == "" {
		return nil, errors.New("storage bucket is required")
	}
	if maxUploadBytes <= 0 {
		return nil, errors.New("storage max upload bytes must be positive")
	}
	if backend == nil {
		return nil, errors.New("storage backend is required")
	}
	return &Service{
		backendName:    backendName,
		bucket:         bucket,
		maxUploadBytes: maxUploadBytes,
		backend:        backend,
	}, nil
}

func NewLocalService(root, bucket string, maxUploadBytes int64) (*Service, error) {
	backend, err := NewLocalBackend(root)
	if err != nil {
		return nil, err
	}
	return NewService("local", bucket, maxUploadBytes, backend)
}

func (s *Service) MaxUploadBytes() int64 {
	return s.maxUploadBytes
}

func (s *Service) BackendName() string {
	return s.backendName
}

func (s *Service) Bucket() string {
	return s.bucket
}

func (s *Service) ObjectKey(projectID, objectID uuid.UUID) string {
	return fmt.Sprintf("projects/%s/objects/%s", projectID, objectID)
}

func (s *Service) UserProfileImageKey(userID, objectID uuid.UUID, variant string) string {
	return fmt.Sprintf("users/%s/profile-images/%s/%s", userID, objectID, variant)
}

func (s *Service) ProjectImageKey(projectID, objectID uuid.UUID, variant string) string {
	return fmt.Sprintf("projects/%s/images/%s/%s", projectID, objectID, variant)
}

func (s *Service) Put(ctx context.Context, projectID, objectID uuid.UUID, r io.Reader) (StoredObject, error) {
	key := s.ObjectKey(projectID, objectID)
	return s.PutKey(ctx, key, r)
}

func (s *Service) PutUserProfileImage(ctx context.Context, userID, objectID uuid.UUID, variant string, r io.Reader) (StoredObject, error) {
	key := s.UserProfileImageKey(userID, objectID, variant)
	return s.PutKey(ctx, key, r)
}

func (s *Service) PutProjectImage(ctx context.Context, projectID, objectID uuid.UUID, variant string, r io.Reader) (StoredObject, error) {
	key := s.ProjectImageKey(projectID, objectID, variant)
	return s.PutKey(ctx, key, r)
}

func (s *Service) PutKey(ctx context.Context, key string, r io.Reader) (StoredObject, error) {
	written, err := s.backend.Put(ctx, key, r, s.maxUploadBytes)
	if err != nil {
		return StoredObject{}, err
	}
	return StoredObject{
		Backend:   s.backendName,
		Bucket:    s.bucket,
		ObjectKey: key,
		ByteSize:  written.Size,
		SHA256:    written.SHA256,
	}, nil
}

func (s *Service) Open(ctx context.Context, key string) (io.ReadCloser, error) {
	return s.backend.Open(ctx, key)
}

func (s *Service) Delete(ctx context.Context, key string) error {
	return s.backend.Delete(ctx, key)
}
