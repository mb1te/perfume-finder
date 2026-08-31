package registry

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func TestImportDirRequiresEveryPage(t *testing.T) {
	dir := t.TempDir()
	for page := 1; page <= 16; page++ {
		html := fmt.Sprintf(`<div class="post"><div class="entry-content">https://shop%d.ru</div></div>`, page)
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("page-%02d.html", page)), []byte(html), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	items, err := ImportDir(context.Background(), dir, 16, "https://topic")
	if err != nil || len(items) != 16 {
		t.Fatalf("items=%d err=%v", len(items), err)
	}
	_ = os.Remove(filepath.Join(dir, "page-16.html"))
	if _, err := ImportDir(context.Background(), dir, 16, "https://topic"); err == nil {
		t.Fatal("missing page accepted")
	}
}
