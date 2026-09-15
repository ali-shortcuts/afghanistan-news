package fetcher

import (
	"context"
	"fmt"
	"net"
	"strings"
)

// SSRFGuard blocks outbound requests from the ingestion worker to private, loopback,
// link-local and cloud-metadata addresses. Feed URLs are external input, so the fetcher
// is treated as SSRF-sensitive infrastructure (§48, §153). This is a launch blocker (§317).
type SSRFGuard struct {
	AllowPrivate bool // test-only escape hatch
}

// blockedNetworks enumerate address space that must never be reachable from the worker.
var blockedNetworks = func() []*net.IPNet {
	cidrs := []string{
		"0.0.0.0/8", "10.0.0.0/8", "100.64.0.0/10", "127.0.0.0/8", "169.254.0.0/16",
		"172.16.0.0/12", "192.0.0.0/24", "192.0.2.0/24", "192.168.0.0/16", "198.18.0.0/15",
		"198.51.100.0/24", "203.0.113.0/24", "224.0.0.0/4", "240.0.0.0/4",
		"::1/128", "fc00::/7", "fe80::/10", "ff00::/8", "::/128",
	}
	out := make([]*net.IPNet, 0, len(cidrs))
	for _, c := range cidrs {
		if _, n, err := net.ParseCIDR(c); err == nil {
			out = append(out, n)
		}
	}
	return out
}()

// blockedHosts are hostnames that resolve to infrastructure rather than publishers.
var blockedHosts = map[string]bool{
	"localhost":                true,
	"metadata":                 true,
	"metadata.google.internal": true,
	"instance-data":            true,
}

// allowedPorts restricts ingestion to normal web ports.
var allowedPorts = map[string]bool{"": true, "80": true, "443": true, "8080": true, "8443": true}

// CheckHost validates a hostname/port pair, resolving DNS and rejecting private targets.
func (g *SSRFGuard) CheckHost(ctx context.Context, host string) error {
	if g.AllowPrivate {
		return nil
	}
	hostname := host
	port := ""
	if h, p, err := net.SplitHostPort(host); err == nil {
		hostname, port = h, p
	}
	hostname = strings.ToLower(strings.TrimSuffix(hostname, "."))

	if blockedHosts[hostname] {
		return fmt.Errorf("ssrf: host %q is not permitted", hostname)
	}
	if strings.HasSuffix(hostname, ".local") || strings.HasSuffix(hostname, ".internal") ||
		strings.HasSuffix(hostname, ".localhost") || strings.HasSuffix(hostname, ".home.arpa") {
		return fmt.Errorf("ssrf: internal hostname %q is not permitted", hostname)
	}
	if !allowedPorts[port] {
		return fmt.Errorf("ssrf: port %q is not permitted", port)
	}

	ips, err := net.DefaultResolver.LookupIPAddr(ctx, hostname)
	if err != nil {
		return fmt.Errorf("dns: %w", err)
	}
	if len(ips) == 0 {
		return fmt.Errorf("dns: no addresses for %q", hostname)
	}
	for _, ip := range ips {
		if err := g.CheckIP(ip.IP); err != nil {
			return err
		}
	}
	return nil
}

// CheckIP rejects a single resolved address.
func (g *SSRFGuard) CheckIP(ip net.IP) error {
	if g.AllowPrivate {
		return nil
	}
	if ip == nil {
		return fmt.Errorf("ssrf: nil address")
	}
	if ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
		ip.IsInterfaceLocalMulticast() || ip.IsUnspecified() {
		return fmt.Errorf("ssrf: address %s is not permitted", ip)
	}
	for _, n := range blockedNetworks {
		if n.Contains(ip) {
			return fmt.Errorf("ssrf: address %s is in blocked range %s", ip, n.String())
		}
	}
	// Cloud metadata endpoints.
	if ip.String() == "169.254.169.254" || ip.String() == "fd00:ec2::254" {
		return fmt.Errorf("ssrf: cloud metadata address is not permitted")
	}
	return nil
}
