package storage

import (
	"context"
	"database/sql"
	"fmt"
	"parfumes_finder/internal/registry"
	"time"
)

type Registry struct{ db *sql.DB }

func NewRegistry(db *sql.DB) *Registry { return &Registry{db} }
func (r *Registry) ImportEvidence(ctx context.Context, items []registry.Evidence) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	for _, e := range items {
		_, err = tx.ExecContext(ctx, `INSERT INTO shops(shop_id,display_name,network_domain,enabled,trust_state,updated_at) VALUES(?,?,?,0,'candidate',?) ON CONFLICT(shop_id) DO UPDATE SET display_name=excluded.display_name,network_domain=excluded.network_domain,updated_at=excluded.updated_at`, e.NetworkDomain, e.DisplayDomain, e.NetworkDomain, time.Now().UnixNano())
		if err != nil {
			return err
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO shop_evidence(idempotency_key,network_domain,display_domain,related_network_domain,related_display_domain,kind,page,post_url,observed_at,excerpt) VALUES(?,?,?,?,?,?,?,?,?,?) ON CONFLICT(idempotency_key) DO NOTHING`, e.ID, e.NetworkDomain, e.DisplayDomain, e.RelatedNetworkDomain, e.RelatedDisplayDomain, e.Kind, e.Page, e.PostURL, e.ObservedAt.UnixNano(), e.Excerpt)
		if err != nil {
			return err
		}
		if e.Kind == registry.EvidenceAlias && e.RelatedNetworkDomain != "" {
			_, err = tx.ExecContext(ctx, `INSERT INTO shop_aliases(network_domain,related_network_domain,evidence_id) VALUES(?,?,?) ON CONFLICT(evidence_id) DO NOTHING`, e.NetworkDomain, e.RelatedNetworkDomain, e.ID)
			if err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}
func (r *Registry) SetTrust(ctx context.Context, network, display string, trust registry.TrustState, enabled bool) error {
	_, err := r.db.ExecContext(ctx, `INSERT INTO shops(shop_id,display_name,network_domain,enabled,trust_state,updated_at) VALUES(?,?,?,?,?,?) ON CONFLICT(shop_id) DO UPDATE SET display_name=excluded.display_name,enabled=excluded.enabled,trust_state=excluded.trust_state,updated_at=excluded.updated_at`, network, display, network, enabled, trust, time.Now().UnixNano())
	return err
}
func (r *Registry) ListShops(ctx context.Context) ([]registry.Shop, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT network_domain,display_name,trust_state,enabled FROM shops ORDER BY network_domain`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var shops []registry.Shop
	for rows.Next() {
		var s registry.Shop
		if err := rows.Scan(&s.NetworkDomain, &s.DisplayDomain, &s.TrustState, &s.Enabled); err != nil {
			return nil, err
		}
		shops = append(shops, s)
	}
	return shops, rows.Err()
}
func (r *Registry) EvidenceForDomain(ctx context.Context, domain string) ([]registry.Evidence, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT idempotency_key,network_domain,display_domain,related_network_domain,related_display_domain,kind,page,post_url,observed_at,excerpt FROM shop_evidence WHERE network_domain=? ORDER BY page,idempotency_key`, domain)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []registry.Evidence
	for rows.Next() {
		var e registry.Evidence
		var observed int64
		if err := rows.Scan(&e.ID, &e.NetworkDomain, &e.DisplayDomain, &e.RelatedNetworkDomain, &e.RelatedDisplayDomain, &e.Kind, &e.Page, &e.PostURL, &observed, &e.Excerpt); err != nil {
			return nil, fmt.Errorf("scan evidence: %w", err)
		}
		e.ObservedAt = time.Unix(0, observed)
		items = append(items, e)
	}
	return items, rows.Err()
}
