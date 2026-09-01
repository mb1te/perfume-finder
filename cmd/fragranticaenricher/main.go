package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chromedp/chromedp"

	"parfumes_finder/internal/fragrantica"
)

const (
	sidecarAddress   = ":8081"
	healthcheckURL   = "http://127.0.0.1:8081/healthz"
	healthcheckLimit = 3 * time.Second
)

type httpDoer interface {
	Do(*http.Request) (*http.Response, error)
}

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if err := run(ctx, os.Args[1:]); err != nil {
		log.Printf("fragrantica enricher: %v", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, args []string) error {
	if len(args) == 1 && args[0] == "healthcheck" {
		return runHealthcheck(ctx, http.DefaultClient)
	}
	if len(args) != 0 {
		return fmt.Errorf("usage: fragranticaenricher [healthcheck]")
	}

	allocatorOptions := chromeAllocatorOptions(os.Getenv("CHROME_NO_SANDBOX"))
	allocatorCtx, allocatorCancel := chromedp.NewExecAllocator(ctx, allocatorOptions...)
	defer allocatorCancel()
	renderer, err := fragrantica.NewBrowserRenderer(allocatorCtx, nil)
	if err != nil {
		return fmt.Errorf("start Chromium: %w", err)
	}
	defer renderer.Close()
	server := &http.Server{
		Addr:              sidecarAddress,
		Handler:           fragrantica.NewServer(renderer),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       20 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       30 * time.Second,
		MaxHeaderBytes:    16 * 1024,
	}

	serveResult := make(chan error, 1)
	go func() {
		serveResult <- server.ListenAndServe()
	}()
	select {
	case err := <-serveResult:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return err
		}
		err := <-serveResult
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	}
}

func chromeAllocatorOptions(noSandbox string) []chromedp.ExecAllocatorOption {
	options := append([]chromedp.ExecAllocatorOption(nil), chromedp.DefaultExecAllocatorOptions[:]...)
	if noSandbox == "1" {
		options = append(options, chromedp.NoSandbox)
	}
	return options
}

func runHealthcheck(ctx context.Context, client httpDoer) error {
	probeCtx, cancel := context.WithTimeout(ctx, healthcheckLimit)
	defer cancel()
	request, err := http.NewRequestWithContext(probeCtx, http.MethodGet, healthcheckURL, nil)
	if err != nil {
		return err
	}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 4*1024))
	if response.StatusCode != http.StatusOK {
		return fmt.Errorf("health endpoint returned HTTP %d", response.StatusCode)
	}
	return nil
}
