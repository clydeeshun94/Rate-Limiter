package identity

import (
	"net"
	"net/http/httptest"
	"testing"
)

func mustNetwork(t *testing.T, cidr string) *net.IPNet {
	t.Helper()
	_, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	return n
}

func TestHTTPResolverPrecedence(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.test", nil)
	r.RemoteAddr = "10.0.0.8:1234"
	r.Header.Set("X-Authenticated-User", " user-42 ")
	got, err := (HTTPResolver{Config: Config{TrustedProxies: []*net.IPNet{mustNetwork(t, "10.0.0.0/8")}}}).Resolve(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Primary != "user-42" || got.Source != "authenticated_user" {
		t.Fatalf("unexpected identity: %+v", got)
	}
}

func TestHTTPResolverDoesNotTrustForwardedHeaderFromUntrustedPeer(t *testing.T) {
	r := httptest.NewRequest("GET", "http://example.test", nil)
	r.RemoteAddr = "192.0.2.8:1234"
	r.Header.Set("X-Forwarded-For", "203.0.113.9")
	got, err := (HTTPResolver{}).Resolve(r)
	if err != nil {
		t.Fatal(err)
	}
	if got.Primary != "ip:192.0.2.8" {
		t.Fatalf("spoofed forwarded address accepted: %+v", got)
	}
}

func TestNetworkIdentity(t *testing.T) {
	if got := NetworkIdentity("192.0.2.44", 24, 64); got != "192.0.2.0/24" {
		t.Fatalf("got %q", got)
	}
	if got := NetworkIdentity("2001:db8::42", 24, 64); got != "2001:db8::/64" {
		t.Fatalf("got %q", got)
	}
}
