package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"parfumes_finder/internal/app"
	"parfumes_finder/internal/config"
	"parfumes_finder/internal/enrichment"
	"parfumes_finder/internal/health"
	"parfumes_finder/internal/registryhealth"
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
		address := os.Getenv("HTTP_ADDR")
		if address == "" {
			address = "127.0.0.1:8080"
		}
		resp, err := http.Get(app.HealthcheckURL(address))
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
	var enricher enrichment.Service
	if cfg.FragranticaEnricherURL != "" {
		remote, err := enrichment.NewHTTPClient(cfg.FragranticaEnricherURL, &http.Client{Timeout: 15 * time.Second})
		if err != nil {
			return err
		}
		enricher = enrichment.NewCachedService(remote, storage.NewEnrichmentCache(db, 7*24*time.Hour, clock))
	}
	transport, err := tg.NewTransport(cfg.TelegramToken, storage.NewSessions(db), coordinator, enricher)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()
	go health.NewScheduler(adapters, storage.NewHealth(db, clock), cfg.HealthInterval).Run(ctx)
	registryRepo := storage.NewRegistry(db)
	registryChecker := registryhealth.NewChecker(nil, cfg.SearchTimeout)
	go registryhealth.NewScheduler(registryRepo, registryChecker, storage.NewHealth(db, clock)).Run(ctx, cfg.HealthInterval)
	readiness := app.NewReadiness()
	server := &http.Server{Addr: cfg.HTTPAddr, Handler: readiness.Handler()}
	serverErr := make(chan error, 1)
	go func() {
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			serverErr <- err
			cancel()
		}
	}()
	go func() {
		<-ctx.Done()
		shutdownCtx, stop := context.WithTimeout(context.Background(), 5*time.Second)
		defer stop()
		_ = server.Shutdown(shutdownCtx)
	}()
	readiness.MarkReady()
	transport.Start(ctx)
	readiness.MarkNotReady()
	select {
	case err := <-serverErr:
		return err
	default:
		return nil
	}
}
