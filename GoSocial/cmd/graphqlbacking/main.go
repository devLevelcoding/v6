// Command graphqlbacking runs the graphql-backing service as its own
// process: /users/{id} and /posts/{id} HTTP endpoints
// (internal/graphqlbacking) backed by the main go-social-v2 API's live
// event-sourced state, fetched over HTTP (internal/statefetch) rather
// than shared memory.
package main

import (
	"fmt"
	"log"
	"net/http"

	"gosocial/internal/env"
	"gosocial/internal/graphqlbacking"
	"gosocial/internal/statefetch"
)

func main() {
	addr := env.Getenv("ADDR", ":8452")
	socialAPIAddr := env.Getenv("SOCIAL_API_ADDR", "http://localhost:8400")

	fetcher := statefetch.New(socialAPIAddr)
	svc := graphqlbacking.New(fetcher.State)

	fmt.Printf("go-social-v2 graphql-backing listening on %s, backed by %s\n", addr, socialAPIAddr)
	log.Fatal(http.ListenAndServe(addr, svc.Mux()))
}
