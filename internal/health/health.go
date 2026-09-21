// Package health serves a minimal HTTP health check (GET /health -> 200).
// Only used for container liveness/readiness; no other endpoints.
package health

import (
	"fmt"
	"net"
	"net/http"
)

// Addr joins host + port ("0.0.0.0:8080").
func Addr(host string, port int) string {
	return net.JoinHostPort(host, fmt.Sprint(port))
}

// Handler serves /health with {"status":"ok"}; everything else 404.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})
	return mux
}

// Serve blocks serving Handler() on addr.
func Serve(addr string) error {
	return http.ListenAndServe(addr, Handler())
}
