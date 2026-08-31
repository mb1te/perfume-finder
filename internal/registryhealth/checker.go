package registryhealth

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"time"

	"parfumes_finder/internal/registry"
	"parfumes_finder/internal/shop"
)

type Resolver interface {
	LookupIP(context.Context, string, string) ([]net.IP, error)
}
type Checker struct {
	client   *http.Client
	resolver Resolver
}

func NewChecker(resolver Resolver, timeout time.Duration) *Checker {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	checker := &Checker{resolver: resolver}
	transport := &http.Transport{Proxy: nil, DialContext: checker.safeDialContext}
	checker.client = &http.Client{Transport: transport, Timeout: timeout, CheckRedirect: func(request *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return fmt.Errorf("too many redirects")
		}
		return checker.validateTarget(request.Context(), request.URL.String())
	}}
	return checker
}

func newCheckerWithClient(client *http.Client, resolver Resolver) *Checker {
	return &Checker{client: client, resolver: resolver}
}

func (c *Checker) Check(ctx context.Context, target registry.Shop) shop.HealthResult {
	checked := time.Now()
	endpoint := "https://" + target.NetworkDomain + "/"
	if err := c.validateTarget(ctx, endpoint); err != nil {
		return shop.HealthResult{Status: shop.HealthDegraded, Err: err, CheckedAt: checked}
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
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
	if response.StatusCode >= 200 && response.StatusCode < 300 {
		status := shop.HealthHealthy
		if canonical != endpoint {
			status = shop.HealthRedirected
		}
		return shop.HealthResult{Status: status, CanonicalURL: canonical, CheckedAt: checked}
	}
	return shop.HealthResult{Status: shop.HealthDegraded, CanonicalURL: canonical, Err: fmt.Errorf("HTTP %d", response.StatusCode), CheckedAt: checked}
}

func (c *Checker) validateTarget(ctx context.Context, raw string) error {
	target, err := url.Parse(raw)
	if err != nil || (target.Scheme != "http" && target.Scheme != "https") || target.Hostname() == "" {
		return fmt.Errorf("invalid redirect target")
	}
	ips, err := c.resolver.LookupIP(ctx, "ip", target.Hostname())
	if err != nil {
		return fmt.Errorf("resolve %s: %w", target.Hostname(), err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("resolve %s: no addresses", target.Hostname())
	}
	for _, ip := range ips {
		if !isPublic(ip) {
			return fmt.Errorf("%s resolves to non-public address", target.Hostname())
		}
	}
	return nil
}

func (c *Checker) safeDialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := c.resolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if isPublic(ip) {
			return (&net.Dialer{}).DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		}
	}
	return nil, fmt.Errorf("%s has no public address", host)
}

func isPublic(ip net.IP) bool {
	return ip != nil && !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}
