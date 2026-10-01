package middleware

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
)

// ParseTrustedProxies parses server.trusted_proxies entries: CIDRs or bare IPs.
func ParseTrustedProxies(entries []string) ([]netip.Prefix, error) {
	var prefixes []netip.Prefix
	for _, e := range entries {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		if strings.Contains(e, "/") {
			p, err := netip.ParsePrefix(e)
			if err != nil {
				return nil, fmt.Errorf("server.trusted_proxies: %q is not a CIDR: %w", e, err)
			}
			prefixes = append(prefixes, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(e)
		if err != nil {
			return nil, fmt.Errorf("server.trusted_proxies: %q is not an IP address: %w", e, err)
		}
		a = a.Unmap()
		prefixes = append(prefixes, netip.PrefixFrom(a, a.BitLen()))
	}
	return prefixes, nil
}

// ClientIP replaces r.RemoteAddr with the client's address. X-Forwarded-For is
// believed only from trusted proxies, and read right to left: any client can
// prepend hops, so the first untrusted hop from the right is the client.
func ClientIP(trusted []netip.Prefix) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if addr, err := netip.ParseAddr(RemoteIP(r)); err == nil {
				r.RemoteAddr = forwardedClient(addr.Unmap(), r.Header.Values("X-Forwarded-For"), trusted).String()
			}
			next.ServeHTTP(w, r)
		})
	}
}

func forwardedClient(peer netip.Addr, xff []string, trusted []netip.Prefix) netip.Addr {
	var hops []string
	for _, v := range xff {
		hops = append(hops, strings.Split(v, ",")...)
	}
	client := peer
	for i := len(hops) - 1; i >= 0 && isTrusted(client, trusted); i-- {
		hop, err := netip.ParseAddr(strings.TrimSpace(hops[i]))
		if err != nil {
			break
		}
		client = hop.Unmap()
	}
	return client
}

func isTrusted(a netip.Addr, trusted []netip.Prefix) bool {
	for _, p := range trusted {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// RemoteIP returns r.RemoteAddr without its port. After ClientIP runs it is the
// client's address.
func RemoteIP(r *http.Request) string {
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}
