// Package env provides small helpers for reading process configuration.
package env

import "os"

// Getenv returns the value of the environment variable key, or def if it's
// unset or empty.
func Getenv(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
