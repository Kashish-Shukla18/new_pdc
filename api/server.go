package api

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"pdc/config"
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
func StartServer(addr string, db *store.Store, m *manager.PMUManager) {
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

			if err := db.SavePMU(r.Context(), cfg); err != nil {
				http.Error(w, err.Error(), http.StatusInternalServerError)
				return
			}

			// Try to stop if already running, then start (update case)
			_ = m.StopPMU(cfg.Name)
			if err := m.StartPMU(context.Background(), cfg); err != nil {
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

	go http.ListenAndServe(addr, corsMiddleware(mux))
}
