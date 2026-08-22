package cluster

import (
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
)

var ErrBlockedAddress = errors.New("cluster address is not allowed")

// Networks that must never be reachable through a pasted kubeconfig. These are the
// addresses an attacker would aim at to make Kubby read something it should not.
//
// Private ranges are deliberately NOT here: on-prem clusters live on 10/8 and friends,
// so blocking them would block the product's whole purpose. The threat handled here is
// tricking Kubby into probing infrastructure that is not a cluster at all.
var blockedNetworks = []struct {
	cidr   string
	reason string
}{
	{"169.254.0.0/16", "link-local, includes cloud instance metadata"},
	{"fe80::/10", "IPv6 link-local"},
	{"0.0.0.0/8", "unspecified"},
	{"::/128", "unspecified"},
	{"224.0.0.0/4", "multicast"},
	{"ff00::/8", "IPv6 multicast"},
	{"100.64.0.0/10", "carrier-grade NAT"},
}

// AddressPolicy decides which cluster endpoints may be contacted.
type AddressPolicy struct {
	// AllowLoopback permits 127.0.0.0/8 and ::1. Off by default; needed for local
	// development against a cluster running on the same host.
	AllowLoopback bool
}

// resolvedEndpoint is a validated server address plus the addresses it resolved to.
type resolvedEndpoint struct {
	URL   *url.URL
	Host  string
	Addrs []net.IP
}

// validateServerURL parses and vets a kubeconfig server address.
//
// The resolved addresses are returned so the caller can pin them: re-resolving later
// would reopen a DNS rebinding window between validation and use.
func (p AddressPolicy) validateServerURL(raw string) (*resolvedEndpoint, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return nil, fmt.Errorf("%w: %q is not a valid URL", ErrBlockedAddress, raw)
	}
	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return nil, fmt.Errorf("%w: scheme must be https (or http), got %q", ErrBlockedAddress, parsed.Scheme)
	}
	host := parsed.Hostname()
	if host == "" {
		return nil, fmt.Errorf("%w: address has no host", ErrBlockedAddress)
	}

	addrs, err := resolveHost(host)
	if err != nil {
		return nil, err
	}

	// Every resolved address must pass: a name that resolves to both a routable and a
	// blocked address must not be usable.
	for _, ip := range addrs {
		if err := p.checkIP(ip); err != nil {
			return nil, err
		}
	}
	return &resolvedEndpoint{URL: parsed, Host: host, Addrs: addrs}, nil
}

func (p AddressPolicy) checkIP(ip net.IP) error {
	if ip.IsLoopback() {
		if p.AllowLoopback {
			return nil
		}
		return fmt.Errorf("%w: %s is a loopback address", ErrBlockedAddress, ip)
	}

	for _, blocked := range blockedNetworks {
		_, network, err := net.ParseCIDR(blocked.cidr)
		if err != nil {
			continue
		}
		if network.Contains(ip) {
			return fmt.Errorf("%w: %s is %s", ErrBlockedAddress, ip, blocked.reason)
		}
	}
	return nil
}

// resolveHost returns the addresses a host resolves to, or the literal address itself.
func resolveHost(host string) ([]net.IP, error) {
	if ip := net.ParseIP(host); ip != nil {
		return []net.IP{ip}, nil
	}

	addrs, err := net.LookupIP(host)
	if err != nil {
		return nil, fmt.Errorf("%w: cannot resolve %q: %v", ErrBlockedAddress, host, err)
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("%w: %q resolved to no addresses", ErrBlockedAddress, host)
	}
	return addrs, nil
}
