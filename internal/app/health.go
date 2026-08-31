package app

import (
	"fmt"
	"net"
	"net/http"
	"sync/atomic"
)

type Readiness struct{ ready atomic.Bool }

func NewReadiness() *Readiness     { return &Readiness{} }
func (r *Readiness) MarkReady()    { r.ready.Store(true) }
func (r *Readiness) MarkNotReady() { r.ready.Store(false) }
func (r *Readiness) Handler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/healthz" {
			http.NotFound(w, request)
			return
		}
		if !r.ready.Load() {
			http.Error(w, "not ready", http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})
}

func HealthcheckURL(address string) string {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return "http://127.0.0.1:8080/healthz"
	}
	if host == "" || host == "0.0.0.0" || host == "::" {
		host = "127.0.0.1"
	}
	return "http://" + net.JoinHostPort(host, port) + "/healthz"
}
