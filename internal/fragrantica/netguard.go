package fragrantica

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"net/url"
	"strings"
	"sync"
)

var ErrUnsafeURL = errors.New("unsafe Fragrantica URL")

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type NetworkGuard struct {
	resolver Resolver
}

func NewNetworkGuard(resolver Resolver) NetworkGuard {
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	return NetworkGuard{resolver: resolver}
}

func (guard NetworkGuard) Allow(ctx context.Context, target *url.URL) error {
	if target == nil || target.Scheme != "https" || target.User != nil || target.Port() != "" {
		return ErrUnsafeURL
	}
	host := normalizedHostname(target.Hostname())
	if !allowedFragranticaHost(host) {
		return ErrUnsafeURL
	}
	addresses, err := guard.resolver.LookupIPAddr(ctx, host)
	if err != nil || len(addresses) == 0 {
		return ErrUnsafeURL
	}
	for _, address := range addresses {
		if !isPublicIP(address.IP) {
			return ErrUnsafeURL
		}
	}
	return nil
}

func allowedFragranticaHost(host string) bool {
	switch normalizedHostname(host) {
	case "fragrantica.ru", "www.fragrantica.ru", "beta.fragrantica.com", "fimgs.net":
		return true
	default:
		return false
	}
}

func normalizedHostname(host string) string {
	return strings.TrimSuffix(strings.ToLower(host), ".")
}

var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/32"),
	netip.MustParsePrefix("2001:2::/48"),
	netip.MustParsePrefix("2001:10::/28"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

func isPublicIP(ip net.IP) bool {
	address, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() {
		return false
	}
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

type lookupGuard struct {
	guard NetworkGuard
	mu    sync.Mutex
	seen  map[string]struct{}
}

func newLookupGuard(guard NetworkGuard) *lookupGuard {
	return &lookupGuard{guard: guard, seen: make(map[string]struct{})}
}

func (guard *lookupGuard) Allow(ctx context.Context, target *url.URL) error {
	if target == nil || target.Scheme != "https" || target.User != nil || target.Port() != "" || !allowedFragranticaHost(target.Hostname()) {
		return ErrUnsafeURL
	}
	host := normalizedHostname(target.Hostname())
	guard.mu.Lock()
	defer guard.mu.Unlock()
	if _, ok := guard.seen[host]; ok {
		return nil
	}
	if err := guard.guard.Allow(ctx, target); err != nil {
		return err
	}
	guard.seen[host] = struct{}{}
	return nil
}
