package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

const tenantNetworkID = 1

// resource adapts a typed collection to the generic REST handler.
type resource interface {
	list(name string) any
	get(id int) (any, error)
	create(s *Store, body []byte) (any, error)
	update(id int, body []byte) (any, error)
	remove(id int) error
	count() int
}

type adapter[T any] struct{ c **collection[T] }

func (a adapter[T]) list(name string) any                      { return (*a.c).list(name) }
func (a adapter[T]) get(id int) (any, error)                   { return (*a.c).get(id) }
func (a adapter[T]) create(s *Store, body []byte) (any, error) { return (*a.c).create(s, body) }
func (a adapter[T]) update(id int, body []byte) (any, error)   { return (*a.c).update(id, body) }
func (a adapter[T]) remove(id int) error                       { return (*a.c).remove(id) }
func (a adapter[T]) count() int                                { return len((*a.c).items) }

type route struct {
	prefix    string
	res       resource
	paginated bool // list supports ?offset=&limit= with a {data, pagination} envelope
}

func (s *Store) routes() []route {
	tn := fmt.Sprintf("/api/tenantnetworks/%d", tenantNetworkID)
	return []route{
		{tn + "/multi-cloud-gateways", adapter[alkiraMCGW]{&s.gateways}, true},
		{tn + "/multi-cloud-gateway-connectors", adapter[alkiraConnector]{&s.connectors}, false},
		{tn + "/segments", adapter[alkiraSegment]{&s.segments}, false},
		{tn + "/groups", adapter[alkiraGroup]{&s.groups}, false},
		{"/api/tags", adapter[alkiraTag]{&s.tags}, false},
	}
}

type statusWriter struct {
	http.ResponseWriter
	status int
	msg    string
}

func (w *statusWriter) WriteHeader(code int) { w.status = code; w.ResponseWriter.WriteHeader(code) }

// APIHandler serves the subset of the Alkira REST API the provider uses.
func (s *Store) APIHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sw := &statusWriter{ResponseWriter: w, status: 200}
		s.mu.Lock()
		defer s.mu.Unlock()
		s.tick()
		s.serveAPI(sw, r)
		if r.Method != http.MethodGet || sw.status >= 400 {
			s.updated = time.Now()
		}
		s.record(event{Time: time.Now(), Method: r.Method, Path: r.URL.Path, Status: sw.status, Message: sw.msg})
	})
}

func (s *Store) serveAPI(w *statusWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	auth := r.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "api-key ") && !strings.HasPrefix(auth, "basic ") {
		s.writeError(w, &apiError{Status: 401, Message: "missing or malformed Authorization header"})
		return
	}
	path := strings.TrimSuffix(r.URL.Path, "/")
	switch {
	case path == "/api/tenantnetworksummaries" && r.Method == http.MethodGet:
		writeJSON(w, 200, []map[string]any{{"id": tenantNetworkID, "name": "lumen-connect-sandbox", "state": "ACTIVE"}})
		return
	case strings.HasPrefix(path, fmt.Sprintf("/api/tenantnetworks/%d/provision-requests/", tenantNetworkID)):
		writeJSON(w, 200, map[string]any{"id": path[strings.LastIndex(path, "/")+1:], "state": "SUCCESS"})
		return
	case path == "/sandbox/on-ramp-regions":
		writeJSON(w, 200, onRampRegions)
		return
	case path == "/sandbox/reset" && r.Method == http.MethodPost:
		s.reset()
		writeJSON(w, 200, map[string]string{"status": "reset"})
		return
	}

	for _, rt := range s.routes() {
		if path != rt.prefix && !strings.HasPrefix(path, rt.prefix+"/") {
			continue
		}
		idPart := strings.TrimPrefix(strings.TrimPrefix(path, rt.prefix), "/")
		s.serveResource(w, r, rt, idPart)
		s.syncGatewayConnectors()
		return
	}
	s.writeError(w, &apiError{Status: 404, Message: "no sandbox route for " + r.Method + " " + r.URL.Path})
}

func (s *Store) serveResource(w *statusWriter, r *http.Request, rt route, idPart string) {
	if r.URL.Query().Get("provision") == "true" && r.Method != http.MethodGet {
		w.Header().Set("x-provision-request-id", fmt.Sprintf("pr-%d", time.Now().UnixNano()))
	}
	w.Header().Set("x-provision-request-state", "SUCCESS")

	if idPart == "" {
		switch r.Method {
		case http.MethodGet:
			s.writeList(w, r, rt)
		case http.MethodPost:
			body, _ := io.ReadAll(r.Body)
			obj, err := rt.res.create(s, body)
			s.respond(w, 201, obj, err)
		default:
			s.writeError(w, &apiError{Status: 405, Message: r.Method + " not allowed"})
		}
		return
	}

	id, err := strconv.Atoi(idPart)
	if err != nil {
		s.writeError(w, &apiError{Status: 400, Message: fmt.Sprintf("%q is not a valid id", idPart)})
		return
	}
	switch r.Method {
	case http.MethodGet:
		obj, err := rt.res.get(id)
		s.respond(w, 200, obj, err)
	case http.MethodPut:
		body, _ := io.ReadAll(r.Body)
		obj, err := rt.res.update(id, body)
		s.respond(w, 200, obj, err)
	case http.MethodDelete:
		err := rt.res.remove(id)
		s.respond(w, 200, map[string]any{}, err)
	default:
		s.writeError(w, &apiError{Status: 405, Message: r.Method + " not allowed"})
	}
}

func (s *Store) writeList(w *statusWriter, r *http.Request, rt route) {
	q := r.URL.Query()
	items := rt.res.list(q.Get("name"))
	if !rt.paginated || (q.Get("offset") == "" && q.Get("limit") == "") {
		writeJSON(w, 200, items)
		return
	}
	raw, _ := json.Marshal(items)
	var all []json.RawMessage
	_ = json.Unmarshal(raw, &all)
	offset, _ := strconv.Atoi(q.Get("offset"))
	limit, err := strconv.Atoi(q.Get("limit"))
	if err != nil || limit <= 0 {
		limit = 25
	}
	if offset < 0 || offset > len(all) {
		offset = len(all)
	}
	end := offset + limit
	if end > len(all) {
		end = len(all)
	}
	writeJSON(w, 200, map[string]any{
		"data":       all[offset:end],
		"pagination": map[string]int{"offset": offset, "limit": limit, "hits": len(all)},
	})
}

func (s *Store) respond(w *statusWriter, status int, obj any, err error) {
	if err != nil {
		s.writeError(w, err)
		return
	}
	writeJSON(w, status, obj)
}

func (s *Store) writeError(w *statusWriter, err error) {
	var ae *apiError
	if !errors.As(err, &ae) {
		ae = &apiError{Status: 500, Message: err.Error()}
	}
	w.msg = ae.Error()
	writeJSON(w, ae.Status, ae)
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	_ = enc.Encode(v)
}
