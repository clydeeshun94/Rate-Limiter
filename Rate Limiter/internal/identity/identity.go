package identity

import (
	"errors"
	"net"
	"net/http"
	"strings"
)

type Identity struct{ Primary, Secondary, Source string }
type Resolver interface {
	Resolve(*http.Request) (Identity, error)
}

var ErrUnresolved = errors.New("identity could not be resolved")

type Config struct {
	TrustedProxies []*net.IPNet
	MaxLength      int
}
type ResolverFunc func(*http.Request) (Identity, error)

func (f ResolverFunc) Resolve(r *http.Request) (Identity, error) { return f(r) }

// HTTPResolver applies a strict precedence: authenticated principal, API key,
// session, then trusted network address. Forwarded headers are considered only
// when the immediate peer belongs to TrustedProxies.
type HTTPResolver struct{ Config }

func (h HTTPResolver) Resolve(r *http.Request) (Identity, error) {
	max := h.Config.MaxLength
	if max == 0 {
		max = 256
	}
	for _, candidate := range []struct{ value, source string }{
		{r.Header.Get("X-Authenticated-User"), "authenticated_user"},
		{r.Header.Get("X-API-Key"), "api_key"},
		{r.Header.Get("X-Session-ID"), "session"},
	} {
		if v := clean(candidate.value, max); v != "" {
			return Identity{Primary: v, Source: candidate.source}, nil
		}
	}
	ip := trustedIP(r, h.Config.TrustedProxies)
	if ip == "" {
		return Identity{}, ErrUnresolved
	}
	return Identity{Secondary: ip, Primary: "ip:" + ip, Source: "trusted_ip"}, nil
}
func clean(v string, max int) string {
	v = strings.TrimSpace(v)
	if v == "" || len(v) > max || strings.IndexByte(v, 0) >= 0 {
		return ""
	}
	return v
}
func trustedIP(r *http.Request, proxies []*net.IPNet) string {
	remote, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		remote = r.RemoteAddr
	}
	peer := net.ParseIP(remote)
	if peer == nil {
		return ""
	}
	trusted := false
	for _, n := range proxies {
		if n.Contains(peer) {
			trusted = true
			break
		}
	}
	if trusted {
		for _, raw := range strings.Split(r.Header.Get("X-Forwarded-For"), ",") {
			if ip := net.ParseIP(strings.TrimSpace(raw)); ip != nil {
				return normalize(ip)
			}
		}
	}
	return normalize(peer)
}
func normalize(ip net.IP) string {
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.To16().String()
}
func NetworkIdentity(ip string, v4Prefix, v6Prefix int) string {
	parsed := net.ParseIP(ip)
	if parsed == nil {
		return ""
	}
	bits := 128
	if parsed.To4() != nil {
		parsed = parsed.To4()
		bits = 32
		if v4Prefix == 0 {
			v4Prefix = 24
		}
		v6Prefix = v4Prefix
	}
	if v6Prefix == 0 {
		v6Prefix = 64
	}
	prefix := v6Prefix
	if bits == 32 {
		prefix = v4Prefix
	}
	mask := net.CIDRMask(prefix, bits)
	return (&net.IPNet{IP: parsed.Mask(mask), Mask: mask}).String()
}
