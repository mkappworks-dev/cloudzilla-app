package service

import (
	"context"
	"net"
)

// PrivateNetworkError names the host, never the address, so a refusal can't map the internal network.
type PrivateNetworkError struct{ Host string }

func (e *PrivateNetworkError) Error() string {
	return e.Host + " resolves to a private network address"
}

var blockedNets = []*net.IPNet{
	mustCIDR("0.0.0.0/8"),
	mustCIDR("100.64.0.0/10"), // CGNAT; some cloud metadata services live here
}

func mustCIDR(s string) *net.IPNet {
	_, n, err := net.ParseCIDR(s)
	if err != nil {
		panic(err)
	}
	return n
}

func blockedIP(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsUnspecified() || ip.IsLinkLocalUnicast() || ip.IsMulticast() {
		return true
	}
	for _, n := range blockedNets {
		if n.Contains(ip) {
			return true
		}
	}
	return false
}

// resolvePublic refuses the host if any of its addresses is private, since the dialer may pick any of them.
func resolvePublic(ctx context.Context, host string) ([]net.IPAddr, error) {
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	if len(ips) == 0 {
		return nil, &net.DNSError{Err: "no addresses", Name: host, IsNotFound: true}
	}
	for _, ip := range ips {
		if blockedIP(ip.IP) {
			return nil, &PrivateNetworkError{Host: host}
		}
	}
	return ips, nil
}

// dialPublic connects to the addresses it vetted, so a second DNS answer can't
// swap in a private one between the check and the connect.
func dialPublic(ctx context.Context, d *net.Dialer, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	ips, err := resolvePublic(ctx, host)
	if err != nil {
		return nil, err
	}
	var lastErr error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, lastErr
}
