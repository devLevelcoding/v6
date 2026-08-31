package main

import (
	"os"
	"strconv"

	"github.com/levelcodingdev/goflare/internal/project"
)

func slugOf(name string) string { return project.Slugify(name) }

// resolveProject accepts a project id or slug and returns its id ("" for "").
func resolveProject(store project.Store, ref string) (string, error) {
	if ref == "" {
		return "", nil
	}
	if p, err := store.Get(ref); err == nil {
		return p.ID, nil
	}
	p, err := store.BySlug(ref)
	if err != nil {
		return "", err
	}
	return p.ID, nil
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func envInt(key string, def int) int {
	if v := os.Getenv(key); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}

func envBool(key string, def bool) bool {
	if v := os.Getenv(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
	}
	return def
}
