package storage

import (
	"context"
	"parfumes_finder/internal/registry"
	"testing"
)

func TestRegistryImportIsIdempotentAndPreservesTrust(t *testing.T) {
	db := openTestDB(t)
	repo := NewRegistry(db)
	ctx := context.Background()
	if err := repo.SetTrust(ctx, "randewoo.ru", "randewoo.ru", registry.TrustTrusted, true); err != nil {
		t.Fatal(err)
	}
	e := registry.Evidence{ID: "same", NetworkDomain: "randewoo.ru", DisplayDomain: "randewoo.ru", Kind: registry.EvidenceMention, Page: 1}
	if err := repo.ImportEvidence(ctx, []registry.Evidence{e, e}); err != nil {
		t.Fatal(err)
	}
	shops, err := repo.ListShops(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(shops) != 1 || shops[0].TrustState != registry.TrustTrusted || !shops[0].Enabled {
		t.Fatalf("shops=%+v", shops)
	}
	items, err := repo.EvidenceForDomain(ctx, "randewoo.ru")
	if err != nil || len(items) != 1 {
		t.Fatalf("evidence=%+v err=%v", items, err)
	}
}
func TestRegistryAddsUnknownDomainsAsDisabledCandidates(t *testing.T) {
	db := openTestDB(t)
	repo := NewRegistry(db)
	e := registry.Evidence{ID: "new", NetworkDomain: "new-shop.ru", DisplayDomain: "new-shop.ru", Kind: registry.EvidenceMention}
	if err := repo.ImportEvidence(context.Background(), []registry.Evidence{e}); err != nil {
		t.Fatal(err)
	}
	shops, _ := repo.ListShops(context.Background())
	if len(shops) != 1 || shops[0].Enabled || shops[0].TrustState != registry.TrustCandidate {
		t.Fatalf("shops=%+v", shops)
	}
}
