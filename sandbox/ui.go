package main

import (
	_ "embed"
	"encoding/json"
	"net/http"
	"time"
)

//go:embed index.html
var indexHTML []byte

type section struct {
	Key   string `json:"key"`
	Title string `json:"title"`
	Items any    `json:"items"`
}

type snapshot struct {
	Tenant   string    `json:"tenant"`
	Updated  time.Time `json:"updated"`
	Sections []section `json:"sections"`
	Events   []event   `json:"events"`
}

// UIHandler serves the read-only status page and its JSON feed. It never mutates state.
func (s *Store) UIHandler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(indexHTML)
	})
	mux.HandleFunc("/state.json", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "read-only", http.StatusMethodNotAllowed)
			return
		}
		s.mu.Lock()
		s.tick()
		snap := snapshot{
			Tenant:  "lumen-connect-sandbox",
			Updated: s.updated,
			Sections: []section{
				{"gateways", "Multi-Cloud Gateways", s.gateways.list("")},
				{"connectors", "MCGW Connectors (cloud on-ramps)", s.connectors.list("")},
				{"segments", "Segments", s.segments.list("")},
				{"groups", "Groups", s.groups.list("")},
				{"tags", "Billing Tags", s.tags.list("")},
			},
			Events: append([]event(nil), s.events...),
		}
		body, err := json.Marshal(snap)
		s.mu.Unlock()
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	})
	return mux
}
