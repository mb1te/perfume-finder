package config

import (
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("DB_PATH", "")
	t.Setenv("FRAGRANTICA_ENRICHER_URL", "")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DBPath != "data/perfumes.db" || cfg.SearchTimeout != 8*time.Second || cfg.CacheTTL != 15*time.Minute || cfg.HealthInterval != 6*time.Hour || cfg.GlobalConcurrency != 20 || cfg.PerStoreConcurrency != 2 {
		t.Fatalf("unexpected defaults: %+v", cfg)
	}
	if cfg.FragranticaEnricherURL != "" {
		t.Fatalf("FragranticaEnricherURL = %q", cfg.FragranticaEnricherURL)
	}
}

func TestLoadFragranticaEnricherURL(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "test-token")
	t.Setenv("FRAGRANTICA_ENRICHER_URL", "http://fragrantica-enricher:8081")
	cfg, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.FragranticaEnricherURL != "http://fragrantica-enricher:8081" {
		t.Fatalf("FragranticaEnricherURL = %q", cfg.FragranticaEnricherURL)
	}
}
func TestLoadRequiresTelegramToken(t *testing.T) {
	t.Setenv("TELEGRAM_BOT_TOKEN", "")
	if _, err := Load(); err == nil {
		t.Fatal("missing token accepted")
	}
}
