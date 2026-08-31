// Package debug is GoSnow's profiling surface (CoverGo U2): net/http/pprof plus
// the runtime mutex/block profilers, served on a SEPARATE listener that must be
// bound to a private address — never the traffic port.
package debug

import (
	"flag"
	"log"
	"math"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"runtime/debug"
	"strconv"
)

// Options configures the profiling listener. The zero value is inert.
type Options struct {
	// Addr is the listen address for the debug server, e.g. "127.0.0.1:6060".
	// Empty means "do not start it".
	Addr string
	// MutexFraction, if > 0, is passed to runtime.SetMutexProfileFraction:
	// 1 profiles every event, 100 one in a hundred. Off by default — contention
	// profiling has a small always-on cost.
	MutexFraction int
	// BlockRate, if > 0, is passed to runtime.SetBlockProfileRate (nanoseconds:
	// sample a blocking event that blocked >= BlockRate ns; 1 = every event).
	BlockRate int
}

// Flags registers -debug-addr / -mutex-profile / -block-profile, each defaulting
// from <PREFIX>_DEBUG_ADDR / _MUTEX_PROFILE / _BLOCK_PROFILE. Call the returned
// function after flag.Parse() to get the resolved Options.
func Flags(prefix string) func() Options {
	addr := flag.String("debug-addr", os.Getenv(prefix+"_DEBUG_ADDR"),
		"private address for the pprof/debug listener (empty = off), e.g. 127.0.0.1:6060")
	mutex := flag.Int("mutex-profile", envInt(prefix+"_MUTEX_PROFILE"),
		"runtime.SetMutexProfileFraction (0 = off)")
	block := flag.Int("block-profile", envInt(prefix+"_BLOCK_PROFILE"),
		"runtime.SetBlockProfileRate in ns (0 = off)")
	return func() Options {
		return Options{Addr: *addr, MutexFraction: *mutex, BlockRate: *block}
	}
}

func envInt(key string) int {
	if n, err := strconv.Atoi(os.Getenv(key)); err == nil {
		return n
	}
	return 0
}

// Handler is the mux served on Options.Addr: /debug/pprof/* and the standard
// profile endpoints (heap, goroutine, allocs, mutex, block, profile, trace).
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}

// Start applies the profiler rates and, if Addr is set, launches the debug
// listener in a goroutine. It returns immediately; a listen error is logged,
// not fatal — profiling must never take the service down.
// LogRuntime logs the effective GOMAXPROCS / GOMEMLIMIT / GOGC (CoverGo U6) so
// an operator can confirm the container limits took.
func LogRuntime() {
	memLimit := debug.SetMemoryLimit(-1)
	gogc := debug.SetGCPercent(-1)
	debug.SetGCPercent(gogc)
	limit := "off"
	if memLimit != math.MaxInt64 {
		limit = strconv.FormatInt(memLimit, 10)
	}
	log.Printf("runtime config: gomaxprocs=%d gomemlimit_bytes=%s gogc=%d",
		runtime.GOMAXPROCS(0), limit, gogc)
}

func Start(o Options) {
	LogRuntime()
	if o.MutexFraction > 0 {
		runtime.SetMutexProfileFraction(o.MutexFraction)
	}
	if o.BlockRate > 0 {
		runtime.SetBlockProfileRate(o.BlockRate)
	}
	if o.Addr == "" {
		return
	}
	srv := &http.Server{Addr: o.Addr, Handler: Handler()}
	go func() {
		log.Printf("debug/pprof listening on %s (mutex_fraction=%d block_rate_ns=%d)",
			o.Addr, o.MutexFraction, o.BlockRate)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Printf("debug server stopped: %v", err)
		}
	}()
}
