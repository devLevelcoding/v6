// Command gosnowd is the GoSnow server: an HTTP/SQL REST endpoint wired to the
// catalog, storage, warehouse and query components. Phase 0 (walking
// skeleton) — see ../../future.md.
package main

import (
	"context"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/levelcodingdev/gosnow/internal/catalog"
	"github.com/levelcodingdev/gosnow/internal/debug"
	"github.com/levelcodingdev/gosnow/internal/query"
	"github.com/levelcodingdev/gosnow/internal/server"
	"github.com/levelcodingdev/gosnow/internal/storage"
	"github.com/levelcodingdev/gosnow/internal/warehouse"
)

func main() {
	addr := flag.String("addr", envOr("GOSNOW_ADDR", ":8090"), "listen address")
	dataDir := flag.String("data", envOr("GOSNOW_DATA", ""), "local storage dir (empty = in-memory)")
	s3Endpoint := flag.String("s3-endpoint", os.Getenv("GOSNOW_S3_ENDPOINT"), "S3-compatible endpoint host:port — enables S3 storage (MinIO, AWS, R2, Spaces)")
	s3Bucket := flag.String("s3-bucket", envOr("GOSNOW_S3_BUCKET", "gosnow"), "S3 bucket")
	s3Region := flag.String("s3-region", os.Getenv("GOSNOW_S3_REGION"), "S3 region")
	s3Insecure := flag.Bool("s3-insecure", os.Getenv("GOSNOW_S3_INSECURE") != "", "talk to the S3 endpoint over plain HTTP (local MinIO)")
	debugOpts := debug.Flags("GOSNOW")
	flag.Parse()

	debug.Start(debugOpts())

	store, err := buildStore(*s3Endpoint, *s3Bucket, *s3Region, !*s3Insecure, *dataDir)
	if err != nil {
		log.Fatalf("storage: %v", err)
	}
	_ = store // reserved for the execution engine — future.md Phase 2

	cat := catalog.NewMemory()
	whs := warehouse.NewManager()
	eng := query.NewCoordinator(cat)
	srv := server.New(cat, whs, eng)

	httpSrv := &http.Server{
		Addr:              *addr,
		Handler:           srv,
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("gosnowd listening on %s", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	<-ctx.Done()

	log.Printf("shutting down")
	shutCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := httpSrv.Shutdown(shutCtx); err != nil {
		log.Printf("shutdown: %v", err)
	}
}

// buildStore picks a storage backend: S3 if an endpoint is given, else local
// disk if -data is set, else in-memory.
func buildStore(s3Endpoint, s3Bucket, s3Region string, s3Secure bool, dataDir string) (storage.Store, error) {
	switch {
	case s3Endpoint != "":
		s3, err := storage.NewS3Store(context.Background(), storage.S3Config{
			Endpoint:  s3Endpoint,
			Region:    s3Region,
			Bucket:    s3Bucket,
			AccessKey: os.Getenv("GOSNOW_S3_ACCESS_KEY"),
			SecretKey: os.Getenv("GOSNOW_S3_SECRET_KEY"),
			Secure:    s3Secure,
		})
		if err != nil {
			return nil, err
		}
		log.Printf("storage: s3 endpoint=%s bucket=%s secure=%t", s3Endpoint, s3Bucket, s3Secure)
		return s3, nil
	case dataDir != "":
		ls, err := storage.NewLocalStore(dataDir)
		if err != nil {
			return nil, err
		}
		log.Printf("storage: local disk at %s", dataDir)
		return ls, nil
	default:
		log.Printf("storage: in-memory (pass -data, or -s3-endpoint with GOSNOW_S3_ACCESS_KEY/_SECRET_KEY)")
		return storage.NewMemStore(), nil
	}
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
