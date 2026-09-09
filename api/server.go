package api

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strings"
	"time"

	"pdc/config"
	"pdc/internal/instance"
	"pdc/manager"
	"pdc/store"
)

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
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}
			if pmus == nil {
				pmus = []config.PMUConfig{}
			}
			json.NewEncoder(w).Encode(pmus)
		} else if r.Method == "POST" {
			var cfg config.PMUConfig
			if err := json.NewDecoder(r.Body).Decode(&cfg); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			if cfg.Name == "" {
				http.Error(w, "pmu name is required", http.StatusBadRequest)
				return
			}
			if err := validatePMUPorts(r.Context(), db, cfg); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			cfg.Normalize()

			if err := db.SavePMU(r.Context(), cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
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

			w.WriteHeader(http.StatusCreated)
		} else {
			http.Error(w, "Method Not Allowed", http.StatusMethodNotAllowed)
		}
	})

	mux.HandleFunc("/api/pmus/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "DELETE" {
			name := strings.TrimPrefix(r.URL.Path, "/api/pmus/")
			if name == "" {
				http.Error(w, "missing pmu name", http.StatusBadRequest)
				return
			}

			if err := db.DeletePMU(r.Context(), name); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			m.StopPMU(name) // Ignore error if not running
			w.WriteHeader(http.StatusNoContent)
		} else {
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

func validatePMUPorts(ctx context.Context, db *store.Store, incoming config.PMUConfig) error {
	pmus, err := db.GetAllPMUs(ctx)
	if err != nil {
		return err
	}
	for _, p := range pmus {
		if p.Name == incoming.Name {
			continue
		}
		if incoming.Port > 0 && p.Port == incoming.Port {
			return fmt.Errorf("port %d already used by %s", incoming.Port, p.Name)
		}
	}
	return nil
}
