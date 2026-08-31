package storage

import (
	"database/sql"
	"embed"
	"fmt"

	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

func Open(path string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)

	migrations, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("read migrations: %w", err)
	}
	for _, migration := range migrations {
		contents, readErr := migrationFiles.ReadFile("migrations/" + migration.Name())
		if readErr != nil {
			_ = db.Close()
			return nil, fmt.Errorf("read migration %s: %w", migration.Name(), readErr)
		}
		if _, execErr := db.Exec(string(contents)); execErr != nil {
			_ = db.Close()
			return nil, fmt.Errorf("apply migration %s: %w", migration.Name(), execErr)
		}
	}
	return db, nil
}
