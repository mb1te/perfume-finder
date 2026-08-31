package config

import (
	"fmt"
	"os"
	"time"
)

type Config struct {
	TelegramToken, DBPath, HTTPAddr         string
	SearchTimeout, CacheTTL, HealthInterval time.Duration
	GlobalConcurrency, PerStoreConcurrency  int
}

func Load() (Config, error) {
	token := os.Getenv("TELEGRAM_BOT_TOKEN")
	if token == "" {
		return Config{}, fmt.Errorf("TELEGRAM_BOT_TOKEN is required")
	}
	db := os.Getenv("DB_PATH")
	if db == "" {
		db = "data/perfumes.db"
	}
	addr := os.Getenv("HTTP_ADDR")
	if addr == "" {
		addr = "127.0.0.1:8080"
	}
	return Config{TelegramToken: token, DBPath: db, HTTPAddr: addr, SearchTimeout: 8 * time.Second, CacheTTL: 15 * time.Minute, HealthInterval: 6 * time.Hour, GlobalConcurrency: 20, PerStoreConcurrency: 2}, nil
}
