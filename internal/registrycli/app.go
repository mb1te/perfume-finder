package registrycli

import (
	"context"
	"flag"
	"fmt"
	"io"
	"parfumes_finder/internal/registry"
	"parfumes_finder/internal/storage"
	"strconv"
)

func Run(ctx context.Context, args []string, out io.Writer, dbPath string) int {
	db, err := storage.Open(dbPath)
	if err != nil {
		fmt.Fprintln(out, err)
		return 1
	}
	defer db.Close()
	repo := storage.NewRegistry(db)
	if len(args) == 0 {
		fmt.Fprintln(out, "command required")
		return 2
	}
	switch args[0] {
	case "import":
		fs := flag.NewFlagSet("import", flag.ContinueOnError)
		fs.SetOutput(out)
		dir := fs.String("input-dir", "data/fragrantica/topic-235155", "")
		pages := fs.Int("pages", 16, "")
		if fs.Parse(args[1:]) != nil {
			return 2
		}
		if err := seedApproved(ctx, repo); err != nil {
			fmt.Fprintln(out, err)
			return 1
		}
		items, err := registry.ImportDir(ctx, *dir, *pages, "https://www.fragrantica.ru/board/viewtopic.php?id=235155")
		if err == nil {
			err = repo.ImportEvidence(ctx, items)
		}
		if err != nil {
			fmt.Fprintln(out, err)
			return 1
		}
		fmt.Fprintf(out, "imported evidence: %d\n", len(items))
		return 0
	case "list":
		shops, err := repo.ListShops(ctx)
		if err != nil {
			fmt.Fprintln(out, err)
			return 1
		}
		for _, s := range shops {
			fmt.Fprintf(out, "%s\t%s\t%t\n", s.NetworkDomain, s.TrustState, s.Enabled)
		}
		return 0
	case "evidence":
		fs := flag.NewFlagSet("evidence", flag.ContinueOnError)
		domain := fs.String("domain", "", "")
		if fs.Parse(args[1:]) != nil || *domain == "" {
			return 2
		}
		items, err := repo.EvidenceForDomain(ctx, *domain)
		if err != nil {
			return 1
		}
		for _, e := range items {
			fmt.Fprintf(out, "%s\tpage=%d\t%s\t%s\n", e.Kind, e.Page, e.PostURL, e.Excerpt)
		}
		return 0
	case "set-trust":
		fs := flag.NewFlagSet("set-trust", flag.ContinueOnError)
		domain := fs.String("domain", "", "")
		trust := fs.String("trust", "", "")
		enabled := fs.String("enabled", "false", "")
		if fs.Parse(args[1:]) != nil {
			return 2
		}
		state := registry.TrustState(*trust)
		if state != registry.TrustCandidate && state != registry.TrustTrusted && state != registry.TrustBlocked {
			return 2
		}
		on, err := strconv.ParseBool(*enabled)
		if err != nil {
			return 2
		}
		if err := repo.SetTrust(ctx, *domain, *domain, state, on); err != nil {
			return 1
		}
		return 0
	default:
		return 2
	}
}
func seedApproved(ctx context.Context, repo *storage.Registry) error {
	existing, err := repo.ListShops(ctx)
	if err != nil {
		return err
	}
	present := make(map[string]bool, len(existing))
	for _, shop := range existing {
		present[shop.NetworkDomain] = true
	}
	for _, item := range []struct{ network, display string }{{"randewoo.ru", "randewoo.ru"}, {"allureparfum.ru", "allureparfum.ru"}, {"orental.ru", "orental.ru"}, {"xn--d1ai6ai.xn--p1ai", "духи.рф"}, {"aroma-butik.ru", "aroma-butik.ru"}} {
		if !present[item.network] {
			if err := repo.SetTrust(ctx, item.network, item.display, registry.TrustTrusted, true); err != nil {
				return err
			}
		}
	}
	return nil
}
