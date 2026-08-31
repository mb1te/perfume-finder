package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
)

func ImportDir(ctx context.Context, dir string, pages int, topic string) ([]Evidence, error) {
	var result []Evidence
	seen := map[string]bool{}
	for page := 1; page <= pages; page++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		path := filepath.Join(dir, fmt.Sprintf("page-%02d.html", page))
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open page %d: %w", page, err)
		}
		items, parseErr := ParsePage(page, topic, file)
		_ = file.Close()
		if parseErr != nil {
			return nil, fmt.Errorf("parse page %d: %w", page, parseErr)
		}
		for _, item := range items {
			if !seen[item.ID] {
				seen[item.ID] = true
				result = append(result, item)
			}
		}
	}
	return result, nil
}
