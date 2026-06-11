// Package main is the main entry point for the HTTP server that serves
// inference providers.
package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"

	"charm.land/catwalk/internal/providers"
	"github.com/caarlos0/env/v11"
	"github.com/charmbracelet/x/etag"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var counter = promauto.NewCounter(prometheus.CounterOpts{
	Namespace: "catwalk",
	Subsystem: "providers",
	Name:      "requests_total",
	Help:      "Total number of requests to the providers endpoint",
})

var (
	providersJSON []byte
	providersETag string

	deprecatedJSON []byte
)

func init() {
	var err error
	providersJSON, err = json.MarshalIndent(providers.GetAll(), "", "  ")
	if err != nil {
		log.Fatal("Failed to marshal providers:", err)
	}
	providersETag = etag.Of(providersJSON)

	deprecatedJSON, err = json.Marshal(map[string]any{"error": "This endpoint was removed. Please use /v2/providers instead."})
	if err != nil {
		log.Fatal("Failed to marshal deprecated response:", err)
	}
}

func providersHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	etag.Response(w, providersETag)

	if r.Method == http.MethodHead {
		return
	}

	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	counter.Inc()

	if etag.Matches(r, providersETag) {
		w.WriteHeader(http.StatusNotModified)
		return
	}

	if _, err := w.Write(providersJSON); err != nil {
		log.Printf("Error writing response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

func providersHandlerDeprecated(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	if _, err := w.Write(deprecatedJSON); err != nil {
		log.Printf("Error writing response: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}

type config struct {
	Port int `env:"CATWALK_PORT" envDefault:"8080"`
}

func main() {
	var cfg config
	if err := env.Parse(&cfg); err != nil {
		log.Fatal(fmt.Errorf("parse config: %w", err))
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/v2/providers", providersHandler)
	mux.HandleFunc("/providers", providersHandlerDeprecated)
	mux.HandleFunc("/health", health)
	mux.HandleFunc("/healthz", health)
	mux.Handle("/metrics", promhttp.Handler())

	addr := fmt.Sprintf(":%d", cfg.Port)
	server := &http.Server{
		Addr:         addr,
		Handler:      mux,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
		IdleTimeout:  60 * time.Second,
	}

	log.Println("Server starting on", addr)
	if err := server.ListenAndServe(); err != nil {
		log.Fatal("Server failed to start:", err)
	}
}

func health(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte("OK"))
}
