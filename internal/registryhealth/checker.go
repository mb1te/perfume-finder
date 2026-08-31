package registryhealth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"parfumes_finder/internal/registry"
	"parfumes_finder/internal/shop"
	"time"
)

type Resolver interface {
	LookupIP(context.Context, string, string) ([]net.IP, error)
}
type Checker struct {
	client   *http.Client
	resolver Resolver
}

func NewChecker(client *http.Client, resolver Resolver) *Checker {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return &Checker{client, resolver}
}
func (c *Checker) Check(ctx context.Context, target registry.Shop) shop.HealthResult {
	checked := time.Now()
	ips, err := c.resolver.LookupIP(ctx, "ip", target.NetworkDomain)
	if err != nil || len(ips) == 0 {
		return shop.HealthResult{Status: shop.HealthDegraded, Err: fmt.Errorf("resolve domain: %w", err), CheckedAt: checked}
	}
	for _, ip := range ips {
		if !isPublic(ip) {
			return shop.HealthResult{Status: shop.HealthDegraded, Err: fmt.Errorf("domain resolves to non-public address"), CheckedAt: checked}
		}
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://"+target.NetworkDomain+"/", nil)
		if err != nil {
			return shop.HealthResult{Status: shop.HealthDegraded, Err: err, CheckedAt: checked}
		}
		request.Header.Set("Range", "bytes=0-65535")
		request.Header.Set("User-Agent", "PerfumePriceBot/0.1")
		response, err := c.client.Do(request)
		if err != nil {
			return shop.HealthResult{Status: shop.HealthDegraded, Err: err, CheckedAt: checked}
		}
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 65536))
		_ = response.Body.Close()
		canonical := response.Request.URL.String()
		switch {
		case response.StatusCode >= 200 && response.StatusCode < 300:
			return shop.HealthResult{Status: shop.HealthHealthy, CanonicalURL: canonical, CheckedAt: checked}
		case response.StatusCode >= 300 && response.StatusCode < 400:
			return shop.HealthResult{Status: shop.HealthRedirected, CanonicalURL: canonical, CheckedAt: checked}
		default:
			return shop.HealthResult{Status: shop.HealthDegraded, CanonicalURL: canonical, Err: fmt.Errorf("HTTP %d", response.StatusCode), CheckedAt: checked}
		}
	}
	return shop.HealthResult{Status: shop.HealthDegraded, Err: fmt.Errorf("no address checked"), CheckedAt: checked}
}
func isPublic(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}
