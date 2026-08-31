package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"parfumes_finder/internal/config"
	"parfumes_finder/internal/health"
	"parfumes_finder/internal/search"
	"parfumes_finder/internal/shop"
	"parfumes_finder/internal/shop/allure"
	"parfumes_finder/internal/shop/aromabutik"
	"parfumes_finder/internal/shop/duhirf"
	"parfumes_finder/internal/shop/httpx"
	"parfumes_finder/internal/shop/orental"
	"parfumes_finder/internal/shop/randewoo"
	"parfumes_finder/internal/storage"
	tg "parfumes_finder/internal/telegram"
	"path/filepath"
	"syscall"
	"time"
)

func main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		resp, err := http.Get("http://127.0.0.1:8080/healthz")
		if err != nil || resp.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = resp.Body.Close()
		return
	}
	if err := run(); err != nil {
		log.Fatal(err)
	}
}
func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	if dir := filepath.Dir(cfg.DBPath); dir != "." {
		if err := os.MkdirAll(dir, 0o750); err != nil {
			return err
		}
	}
	db, err := storage.Open(cfg.DBPath)
	if err != nil {
		return err
	}
	defer db.Close()
	client := httpx.New(&http.Client{Timeout: cfg.SearchTimeout})
	clock := time.Now
	adapters := []shop.Adapter{randewoo.New(client, clock), allure.New(client, clock), orental.New(client, clock), duhirf.New(client, clock), aromabutik.New(client, clock)}
	coordinator := search.NewCoordinator(adapters, cfg.SearchTimeout, storage.NewCache(db, cfg.CacheTTL, clock))
	transport, err := tg.NewTransport(cfg.TelegramToken, storage.NewSessions(db), coordinator)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go health.NewScheduler(adapters, storage.NewHealth(db, clock), cfg.HealthInterval).Run(ctx)
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/healthz" {
			http.NotFound(w, r)
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintln(w, "ok")
	})}
	go func() { _ = server.ListenAndServe() }()
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
	}()
	transport.Start(ctx)
	return nil
}
