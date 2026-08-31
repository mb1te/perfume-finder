package registrycli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestImportAndListCommands(t *testing.T) {
	dir := t.TempDir()
	html := `<div class="post firstpost"><div class="entry-content">Адреса проверенных магазинов: https://randewoo.ru https://new-shop.ru</div></div>`
	if err := os.WriteFile(filepath.Join(dir, "page-01.html"), []byte(html), 0o600); err != nil {
		t.Fatal(err)
	}
	db := filepath.Join(t.TempDir(), "registry.db")
	var out bytes.Buffer
	if code := Run(context.Background(), []string{"import", "--input-dir", dir, "--pages", "1"}, &out, db); code != 0 {
		t.Fatalf("import code=%d out=%s", code, out.String())
	}
	out.Reset()
	if code := Run(context.Background(), []string{"list"}, &out, db); code != 0 {
		t.Fatal(code)
	}
	text := out.String()
	for _, want := range []string{"randewoo.ru", "trusted", "new-shop.ru", "candidate"} {
		if !strings.Contains(text, want) {
			t.Fatalf("output %q lacks %q", text, want)
		}
	}
}
