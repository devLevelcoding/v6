package storage

import (
	"context"
	"os"
	"testing"
)

// TestS3Store runs the shared Store suite against a real S3-compatible endpoint
// when GOSNOW_S3_TEST_ENDPOINT is set. Spin up a local MinIO first:
//
//	docker run -p 9000:9000 -e MINIO_ROOT_USER=minioadmin \
//	  -e MINIO_ROOT_PASSWORD=minioadmin minio/minio server /data
//
//	GOSNOW_S3_TEST_ENDPOINT=localhost:9000 \
//	GOSNOW_S3_TEST_ACCESS_KEY=minioadmin \
//	GOSNOW_S3_TEST_SECRET_KEY=minioadmin \
//	go test ./internal/storage -run TestS3Store -v
func TestS3Store(t *testing.T) {
	endpoint := os.Getenv("GOSNOW_S3_TEST_ENDPOINT")
	if endpoint == "" {
		t.Skip("set GOSNOW_S3_TEST_ENDPOINT (+ _ACCESS_KEY/_SECRET_KEY) to run")
	}

	ctx := context.Background()
	s, err := NewS3Store(ctx, S3Config{
		Endpoint:  endpoint,
		Region:    os.Getenv("GOSNOW_S3_TEST_REGION"),
		Bucket:    "gosnow-test",
		AccessKey: os.Getenv("GOSNOW_S3_TEST_ACCESS_KEY"),
		SecretKey: os.Getenv("GOSNOW_S3_TEST_SECRET_KEY"),
		Secure:    os.Getenv("GOSNOW_S3_TEST_SECURE") == "1",
	})
	if err != nil {
		t.Fatalf("connect: %v", err)
	}

	// The suite creates a/b.txt and a/c.txt — clear any leftovers first.
	_ = s.Delete(ctx, "a/b.txt")
	_ = s.Delete(ctx, "a/c.txt")

	runStoreSuite(t, s)
}
