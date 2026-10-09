// Package api is the small REST server used by the React dashboard
// to add / edit / delete PMUs in the address book.
package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"pdc/config"
	"pdc/internal/instance"
	"pdc/manager"
	"pdc/store"
)

func writeStoreErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrUnavailable) {
		http.Error(w, err.Error(), http.StatusServiceUnavailable)
		return
	}
	http.Error(w, err.Error(), http.StatusInternalServerError)
}

// corsMiddleware adds basic CORS headers.
func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")

		if r.Method == "OPTIONS" {
			w.WriteHeader(http.StatusOK)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// saveBody is POST /api/pmus. Name is ignored; identity comes from ip/port/protocol.
// Replace is the previous endpoint identity when the operator changes IP/port.
type saveBody struct {
	config.PMUConfig
	Replace string `json:"replace,omitempty"`
}

// StartServer starts the REST API server on the given address.
// Returns an error if the port is already in use (another PDC instance).
func StartServer(ctx context.Context, addr string, db *store.Store, m *manager.PMUManager) error {
	ln, err := instance.ListenOrExit("api", instance.NormalizeAddr(addr))
	if err != nil {
		return err
	}

	mux := http.NewServeMux()

	mux.HandleFunc("/api/pmus", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "GET" {
			pmus, err := db.GetAllPMUs(r.Context())
			if err != nil {
				log.Printf("api GET /api/pmus error: %v", err)
				writeStoreErr(w, err)
				return
			}
			if pmus == nil {
				pmus = []config.PMUConfig{}
			}
			json.NewEncoder(w).Encode(pmus)
		} else if r.Method == "POST" {
			var body saveBody
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			cfg := body.PMUConfig
			cfg.Normalize()
			if cfg.Name == "" {
				http.Error(w, "ip and port are required", http.StatusBadRequest)
				return
			}
			if err := validateEndpointUnique(r.Context(), db, cfg, body.Replace); err != nil {
				if errors.Is(err, store.ErrUnavailable) {
					writeStoreErr(w, err)
					return
				}
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}

			replace := strings.TrimSpace(body.Replace)
			if replace != "" && replace != cfg.Name {
				_ = m.StopPMU(replace)
				if err := db.DeletePMU(r.Context(), replace); err != nil {
					writeStoreErr(w, err)
					return
				}
			}

			if err := db.SavePMU(r.Context(), cfg); err != nil {
				writeStoreErr(w, err)
				return
			}

			// Stop the old receiver and wait for TCP teardown before reconnecting.
			if err := m.StopPMU(cfg.Name); err != nil {
				log.Printf("api POST /api/pmus stop %s: %v (starting fresh)", cfg.Name, err)
			}
			if err := m.StartPMU(ctx, cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusCreated)
			_ = json.NewEncoder(w).Encode(cfg)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/pmus/", func(w http.ResponseWriter, r *http.Request) {
		raw := strings.TrimPrefix(r.URL.Path, "/api/pmus/")
		raw = strings.Trim(raw, "/")
		if raw == "" {
			http.Error(w, "missing pmu identity", http.StatusBadRequest)
			return
		}

		// /api/pmus/{name}/disconnect|connect
		action := ""
		namePart := raw
		if i := strings.LastIndex(raw, "/"); i >= 0 {
			action = raw[i+1:]
			namePart = raw[:i]
		}
		name, err := url.PathUnescape(namePart)
		if err != nil {
			name = namePart
		}
		if name == "" {
			http.Error(w, "missing pmu identity", http.StatusBadRequest)
			return
		}

		switch {
		case r.Method == "POST" && action == "disconnect":
			if err := db.SetPMUActive(r.Context(), name, false); err != nil {
				if strings.Contains(err.Error(), "not found") {
					http.Error(w, err.Error(), http.StatusNotFound)
					return
				}
				writeStoreErr(w, err)
				return
			}
			_ = m.StopPMU(name) // ok if already stopped
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"name": name, "active": false})

		case r.Method == "POST" && action == "connect":
			cfg, err := db.GetPMU(r.Context(), name)
			if err != nil {
				writeStoreErr(w, err)
				return
			}
			if err := db.SetPMUActive(r.Context(), name, true); err != nil {
				writeStoreErr(w, err)
				return
			}
			cfg.Active = true
			if !m.IsConfigured(cfg.Name) {
				if err := m.StartPMU(ctx, cfg); err != nil {
					http.Error(w, err.Error(), http.StatusInternalServerError)
					return
				}
			}
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(cfg)

		case r.Method == "DELETE" && action == "":
			if err := db.DeletePMU(r.Context(), name); err != nil {
				writeStoreErr(w, err)
				return
			}
			_ = m.StopPMU(name) // Ignore error if not running
			w.WriteHeader(http.StatusNoContent)

		default:
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})

	srv := &http.Server{Handler: corsMiddleware(mux)}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	go func() {
		if err := srv.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("api server error: %v", err)
		}
	}()
	return nil
}

func validateEndpointUnique(ctx context.Context, db *store.Store, incoming config.PMUConfig, replace string) error {
	pmus, err := db.GetAllPMUs(ctx)
	if err != nil {
		return err
	}
	key := incoming.EndpointKey()
	for _, p := range pmus {
		if p.Name == incoming.Name {
			continue
		}
		if replace != "" && p.Name == replace {
			continue
		}
		if p.EndpointKey() == key {
			return fmt.Errorf("endpoint %s already registered as %s", incoming.Name, p.DisplayLabel())
		}
	}
	return nil
}
