package registry

import (
	"fmt"
	"golang.org/x/net/idna"
	"net"
	"net/url"
	"strings"
)

func NormalizeDomain(raw string) (string, string, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !(u.Scheme == "http" || u.Scheme == "https") || u.Hostname() == "" {
		return "", "", fmt.Errorf("invalid shop URL")
	}
	host := strings.TrimSuffix(strings.ToLower(u.Hostname()), ".")
	host = strings.TrimPrefix(host, "www.")
	if host == "fragrantica.ru" || strings.HasSuffix(host, ".fragrantica.ru") {
		return "", "", fmt.Errorf("internal Fragrantica URL")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsUnspecified()) {
		return "", "", fmt.Errorf("non-public address")
	}
	network, err := idna.Lookup.ToASCII(host)
	if err != nil {
		return "", "", err
	}
	display, err := idna.Lookup.ToUnicode(network)
	if err != nil {
		return "", "", err
	}
	return display, network, nil
}
