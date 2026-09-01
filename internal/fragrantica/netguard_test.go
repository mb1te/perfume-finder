package fragrantica

import (
	"context"
	"errors"
	"net"
	"net/url"
	"testing"
)

type fakeResolver struct {
	addresses []net.IPAddr
	err       error
	calls     int
}

func (resolver *fakeResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	resolver.calls++
	return resolver.addresses, resolver.err
}

func TestNetworkGuardRejectsPrivateDNSAnswers(t *testing.T) {
	resolver := &fakeResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}}
	guard := NewNetworkGuard(resolver)
	target, _ := url.Parse("https://www.fragrantica.ru/perfume/Dior/Sauvage-31861.html")
	if err := guard.Allow(context.Background(), target); !errors.Is(err, ErrUnsafeURL) {
		t.Fatalf("error = %v, want ErrUnsafeURL", err)
	}
}

func TestNetworkGuardRequiresEveryDNSAnswerToBePublic(t *testing.T) {
	tests := []struct {
		name      string
		addresses []net.IPAddr
		err       error
		allowed   bool
	}{
		{name: "public IPv4", addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}, allowed: true},
		{name: "public IPv6", addresses: []net.IPAddr{{IP: net.ParseIP("2606:4700:4700::1111")}}, allowed: true},
		{name: "mixed answers", addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}, {IP: net.ParseIP("10.0.0.1")}}},
		{name: "carrier NAT", addresses: []net.IPAddr{{IP: net.ParseIP("100.64.0.1")}}},
		{name: "documentation", addresses: []net.IPAddr{{IP: net.ParseIP("192.0.2.1")}}},
		{name: "empty"},
		{name: "resolver error", err: errors.New("DNS failed")},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resolver := &fakeResolver{addresses: test.addresses, err: test.err}
			guard := NewNetworkGuard(resolver)
			target, _ := url.Parse("https://fimgs.net/image.jpg")
			err := guard.Allow(context.Background(), target)
			if test.allowed && err != nil {
				t.Fatalf("public target rejected: %v", err)
			}
			if !test.allowed && !errors.Is(err, ErrUnsafeURL) {
				t.Fatalf("error = %v, want ErrUnsafeURL", err)
			}
		})
	}
}

func TestNetworkGuardRejectsUnapprovedURLShapeBeforeDNS(t *testing.T) {
	for _, raw := range []string{
		"http://www.fragrantica.ru/perfume/Dior/Sauvage.html",
		"https://evil.example/perfume/Dior/Sauvage.html",
		"https://user@www.fragrantica.ru/perfume/Dior/Sauvage.html",
		"https://www.fragrantica.ru:444/perfume/Dior/Sauvage.html",
	} {
		resolver := &fakeResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}
		guard := NewNetworkGuard(resolver)
		target, _ := url.Parse(raw)
		if err := guard.Allow(context.Background(), target); !errors.Is(err, ErrUnsafeURL) {
			t.Fatalf("%q error = %v, want ErrUnsafeURL", raw, err)
		}
		if resolver.calls != 0 {
			t.Fatalf("%q performed DNS before URL rejection", raw)
		}
	}
}

func TestLookupGuardCachesOnlySuccessfulHostResolutions(t *testing.T) {
	publicResolver := &fakeResolver{addresses: []net.IPAddr{{IP: net.ParseIP("8.8.8.8")}}}
	guard := newLookupGuard(NewNetworkGuard(publicResolver))
	first, _ := url.Parse("https://www.fragrantica.ru/search/?query=Sauvage")
	second, _ := url.Parse("https://www.fragrantica.ru/perfume/Dior/Sauvage.html")
	if err := guard.Allow(context.Background(), first); err != nil {
		t.Fatal(err)
	}
	if err := guard.Allow(context.Background(), second); err != nil {
		t.Fatal(err)
	}
	if publicResolver.calls != 1 {
		t.Fatalf("successful resolver calls = %d, want 1", publicResolver.calls)
	}

	privateResolver := &fakeResolver{addresses: []net.IPAddr{{IP: net.ParseIP("127.0.0.1")}}}
	guard = newLookupGuard(NewNetworkGuard(privateResolver))
	for range 2 {
		if err := guard.Allow(context.Background(), first); !errors.Is(err, ErrUnsafeURL) {
			t.Fatalf("private target error = %v", err)
		}
	}
	if privateResolver.calls != 2 {
		t.Fatalf("failed resolver calls = %d, want 2", privateResolver.calls)
	}
}
