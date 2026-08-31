// Package plugin defines the gateway plugin interface and the chain/
// registry machinery that composes plugins around an upstream handler.
package plugin

import "net/http"

// Plugin is the interface that every gateway plugin must implement.
type Plugin interface {
	Name() string
	Process(w http.ResponseWriter, r *http.Request, next http.Handler)
}

// PluginChain wraps an ordered list of plugins and an inner handler into a
// single http.Handler. Plugins are applied left-to-right.
type PluginChain struct {
	plugins []Plugin
	inner   http.Handler
}

// NewChain creates a PluginChain that applies the given plugins in order
// around inner.
func NewChain(inner http.Handler, plugins ...Plugin) *PluginChain {
	return &PluginChain{plugins: plugins, inner: inner}
}

// ServeHTTP executes the plugin chain.
func (pc *PluginChain) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	pc.build(0).ServeHTTP(w, r)
}

func (pc *PluginChain) build(i int) http.Handler {
	if i >= len(pc.plugins) {
		return pc.inner
	}
	next := pc.build(i + 1)
	current := pc.plugins[i]
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		current.Process(w, r, next)
	})
}

// Registry maps plugin names to Plugin instances.
type Registry map[string]Plugin

// Get retrieves a plugin by name. Returns nil if not found.
func (reg Registry) Get(name string) Plugin { return reg[name] }

// Register adds a plugin to the registry.
func (reg Registry) Register(p Plugin) { reg[p.Name()] = p }
