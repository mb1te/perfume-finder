package registryhealth

import (
	"context"
	"io"
	"net"
	"net/http"
	"parfumes_finder/internal/registry"
	"parfumes_finder/internal/shop"
	"strings"
	"testing"
)

func TestCheckerRejectsPrivateResolvedAddress(t *testing.T) {
	checker := NewChecker(&http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		t.Fatal("HTTP called for private address")
		return nil, nil
	})}, fakeResolver{net.ParseIP("127.0.0.1")})
	result := checker.Check(context.Background(), registry.Shop{NetworkDomain: "evil.test"})
	if result.Status != shop.HealthDegraded || result.Err == nil {
		t.Fatalf("result=%+v", result)
	}
}
func TestCheckerClassifiesHealthyAndForbidden(t *testing.T) {
	for _, tc := range []struct {
		status int
		want   shop.HealthStatus
	}{{200, shop.HealthHealthy}, {403, shop.HealthDegraded}, {429, shop.HealthDegraded}} {
		client := &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: tc.status, Body: io.NopCloser(strings.NewReader("ok")), Header: http.Header{}, Request: r}, nil
		})}
		result := NewChecker(client, fakeResolver{net.ParseIP("93.184.216.34")}).Check(context.Background(), registry.Shop{NetworkDomain: "shop.test"})
		if result.Status != tc.want {
			t.Fatalf("status %d => %q", tc.status, result.Status)
		}
	}
}

type fakeResolver struct{ ip net.IP }

func (r fakeResolver) LookupIP(context.Context, string, string) ([]net.IP, error) {
	return []net.IP{r.ip}, nil
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }
