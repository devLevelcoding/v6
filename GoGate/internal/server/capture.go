package server

import (
	"bytes"
	"net/http"
	"sync"

	"github.com/levelcodingdev/gogate/internal/cache"
)

// capturingWriter records a response instead of (or as well as) sending it, so
// the cache can store what the upstream produced. It is pooled (CoverGo U7) —
// one is taken per cache miss and returned once response() has copied the body
// out.
type capturingWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

var capturePool = sync.Pool{New: func() any { return &capturingWriter{header: http.Header{}} }}

func getCapture() *capturingWriter {
	c := capturePool.Get().(*capturingWriter)
	c.status = http.StatusOK
	return c
}

func putCapture(c *capturingWriter) {
	// Drop header entries but keep the map; reset the buffer but keep its
	// backing array for the next miss.
	for k := range c.header {
		delete(c.header, k)
	}
	c.body.Reset()
	capturePool.Put(c)
}

func (c *capturingWriter) Header() http.Header { return c.header }

func (c *capturingWriter) WriteHeader(status int) { c.status = status }

func (c *capturingWriter) Write(b []byte) (int, error) { return c.body.Write(b) }

func (c *capturingWriter) response() *cache.Response {
	return &cache.Response{
		Status: c.status,
		Header: c.header.Clone(),
		Body:   append([]byte(nil), c.body.Bytes()...),
	}
}

// replay writes a stored response to the real ResponseWriter.
func replay(w http.ResponseWriter, r *cache.Response, cacheState string) {
	for k, vs := range r.Header {
		for _, v := range vs {
			w.Header().Add(k, v)
		}
	}
	if cacheState != "" {
		w.Header().Set("X-Cache", cacheState)
	}
	w.WriteHeader(r.Status)
	_, _ = w.Write(r.Body)
}
