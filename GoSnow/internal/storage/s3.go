package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sort"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config configures an S3Store. Endpoint is host[:port] with no scheme
// ("localhost:9000" for MinIO, "s3.amazonaws.com" for AWS,
// "ACCOUNT.r2.cloudflarestorage.com" for Cloudflare R2,
// "REGION.digitaloceanspaces.com" for Spaces).
type S3Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	Secure    bool
}

// S3Store is a Store backed by any S3-compatible object store (AWS S3, MinIO,
// Cloudflare R2, DigitalOcean Spaces, Backblaze B2). It uses the same
// minio-go client that GoAdmin/gofile already depends on. See future.md
// (Phase 1).
type S3Store struct {
	client *minio.Client
	bucket string
}

// NewS3Store connects to the endpoint and ensures the bucket exists.
func NewS3Store(ctx context.Context, cfg S3Config) (*S3Store, error) {
	if cfg.Endpoint == "" || cfg.Bucket == "" {
		return nil, errors.New("storage: s3 endpoint and bucket are required")
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.Secure,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 client: %w", err)
	}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("storage: s3 bucket check: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("storage: s3 make bucket %q: %w", cfg.Bucket, err)
		}
	}
	return &S3Store{client: client, bucket: cfg.Bucket}, nil
}

// Put uploads data under key.
func (s *S3Store) Put(ctx context.Context, key string, data []byte) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/octet-stream"})
	if err != nil {
		return fmt.Errorf("storage: s3 put %q: %w", key, err)
	}
	return nil
}

// Get downloads the object at key.
func (s *S3Store) Get(ctx context.Context, key string) ([]byte, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("storage: s3 get %q: %w", key, err)
	}
	defer func() { _ = obj.Close() }()

	data, err := io.ReadAll(obj)
	if err != nil {
		if isNotFound(err) {
			return nil, ErrNotExist
		}
		return nil, fmt.Errorf("storage: s3 read %q: %w", key, err)
	}
	return data, nil
}

// List returns keys with the given prefix, sorted.
func (s *S3Store) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	for obj := range s.client.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	}) {
		if obj.Err != nil {
			return nil, fmt.Errorf("storage: s3 list %q: %w", prefix, obj.Err)
		}
		keys = append(keys, obj.Key)
	}
	sort.Strings(keys)
	return keys, nil
}

// Delete removes the object at key, returning ErrNotExist if it was absent
// (S3's own DELETE is idempotent; this restores the Store contract).
func (s *S3Store) Delete(ctx context.Context, key string) error {
	if _, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{}); err != nil {
		if isNotFound(err) {
			return ErrNotExist
		}
		return fmt.Errorf("storage: s3 stat %q: %w", key, err)
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("storage: s3 delete %q: %w", key, err)
	}
	return nil
}

func isNotFound(err error) bool {
	resp := minio.ToErrorResponse(err)
	return resp.Code == "NoSuchKey" || resp.StatusCode == 404
}
