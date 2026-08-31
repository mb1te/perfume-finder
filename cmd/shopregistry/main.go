package main

import (
	"context"
	"os"
	"parfumes_finder/internal/registrycli"
)

func main() {
	db := os.Getenv("DB_PATH")
	if db == "" {
		db = "data/perfumes.db"
	}
	os.Exit(registrycli.Run(context.Background(), os.Args[1:], os.Stdout, db))
}
