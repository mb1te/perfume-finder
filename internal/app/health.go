package app

import (
	"fmt"
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
