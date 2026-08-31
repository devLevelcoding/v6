package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/levelcodingdev/goflare/internal/blob"
	"github.com/levelcodingdev/goflare/internal/group"
	"github.com/levelcodingdev/goflare/internal/org"
	"github.com/levelcodingdev/goflare/internal/pg"
	"github.com/levelcodingdev/goflare/internal/project"
	"github.com/levelcodingdev/goflare/internal/snapshot"
)

// storageConfig is the flag set that selects and configures the backend.
type storageConfig struct {
	databaseURL string
	eventsDir   string
	s3          blob.S3Config
	snapPath    string
}

// storageFlags registers the storage flags and returns the config they fill.
func storageFlags() *storageConfig {
	c := &storageConfig{}
	flag.StringVar(&c.databaseURL, "database-url", os.Getenv("GOFLARE_DATABASE_URL"),
		"Postgres DSN — switches to Postgres + blob storage (Phase 1). Empty = in-memory + snapshot.")
	flag.StringVar(&c.eventsDir, "events-dir", envOr("GOFLARE_EVENTS_DIR", ""),
		"local directory for raw event bodies (used with -database-url when no S3 blob store is set)")
	flag.StringVar(&c.snapPath, "snapshot-path", envOr("GOFLARE_SNAPSHOT", ""),
		"JSON file to persist projects/issues/events across restarts (in-memory mode only)")
	flag.StringVar(&c.s3.Endpoint, "blob-endpoint", os.Getenv("GOFLARE_BLOB_ENDPOINT"), "S3-compatible endpoint host[:port] for raw event bodies")
	flag.StringVar(&c.s3.Bucket, "blob-bucket", os.Getenv("GOFLARE_BLOB_BUCKET"), "S3 bucket for raw event bodies")
	flag.StringVar(&c.s3.Region, "blob-region", envOr("GOFLARE_BLOB_REGION", "us-east-1"), "S3 region")
	flag.StringVar(&c.s3.AccessKey, "blob-access-key", os.Getenv("GOFLARE_BLOB_ACCESS_KEY"), "S3 access key")
	flag.StringVar(&c.s3.SecretKey, "blob-secret-key", os.Getenv("GOFLARE_BLOB_SECRET_KEY"), "S3 secret key")
	flag.BoolVar(&c.s3.Secure, "blob-secure", envBool("GOFLARE_BLOB_SECURE", true), "use TLS for the S3 endpoint")
	return c
}

// stores is the assembled persistence layer. memProj and snap are non-nil only
// in the in-memory + snapshot backend.
type stores struct {
	projects project.Store
	orgs     org.Store
	memProj  *project.MemStore
	snap     *snapshot.Store
}

// touch marks the snapshot dirty if one is in use.
func (s stores) touch() {
	if s.snap != nil {
		s.snap.Touch()
	}
}

// openStorage builds the persistence layer from cfg, wiring `groups` to the
// chosen backend. It exits the process on any setup error.
func openStorage(ctx context.Context, log *slog.Logger, cfg storageConfig, groups *group.Store) stores {
	if cfg.databaseURL == "" {
		return openMemStorage(cfg, log, groups)
	}

	db, err := pg.Open(cfg.databaseURL)
	fatal(log, "postgres", err)

	blobs, err := openBlobStore(ctx, cfg.eventsDir, cfg.s3)
	fatal(log, "blob store", err)

	ps, err := project.NewPGStore(db)
	fatal(log, "project pgstore", err)
	fatal(log, "group pgstore", groups.UsePostgres(db, blobs, log))
	os2, err := org.NewPGStore(db)
	fatal(log, "org pgstore", err)

	log.Info("storage: Postgres + blob store", "schema", "goflare")
	return stores{projects: ps, orgs: os2}
}

func openMemStorage(cfg storageConfig, log *slog.Logger, groups *group.Store) stores {
	mem := project.NewMemStore()
	st := stores{projects: mem, orgs: org.NewMemStore(), memProj: mem}
	if cfg.snapPath != "" {
		st.snap = snapshot.New(cfg.snapPath, log)
		if err := st.snap.Load(mem, groups); err != nil {
			log.Error("snapshot load", "err", err)
		}
		groups.SetOnChange(st.snap.Touch)
	}
	return st
}

// openBlobStore returns the object store for raw event bodies: S3 when an
// endpoint+bucket are configured, otherwise a local directory.
func openBlobStore(ctx context.Context, dir string, s3 blob.S3Config) (blob.Store, error) {
	if s3.Endpoint != "" && s3.Bucket != "" {
		return blob.NewS3Store(ctx, s3)
	}
	if dir == "" {
		dir = "./goflare-events"
	}
	return blob.NewLocalStore(dir)
}

// seedProjectInto calls Seed on whichever Store implementation is in use.
func seedProjectInto(s project.Store, name, id, key string) (project.Project, error) {
	type seeder interface {
		Seed(name, platform, id, publicKey string) (project.Project, error)
	}
	if sd, ok := s.(seeder); ok {
		return sd.Seed(name, "", id, key)
	}
	return s.Create(name, "")
}

func fatal(log *slog.Logger, what string, err error) {
	if err != nil {
		log.Error(what, "err", err)
		os.Exit(1)
	}
}
