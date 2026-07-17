package validator

import (
	"errors"
	"fmt"
	"net"
	"strings"
)

// ErrIPNotAllowed reports an IPv4 that may not be used as a monitoring target.
var ErrIPNotAllowed = errors.New("ipv4 is not an allowed monitoring target")

// CIDRValidator restricts monitoring targets to a configured set of networks.
type CIDRValidator struct {
	allowed []*net.IPNet
}

// NewCIDRValidator parses a comma-separated CIDR allowlist. Empty denies all.
func NewCIDRValidator(allowlist string) (*CIDRValidator, error) {
	var allowed []*net.IPNet
	for entry := range strings.SplitSeq(allowlist, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		_, network, err := net.ParseCIDR(entry)
		if err != nil {
			return nil, fmt.Errorf("invalid CIDR %q in allowlist: %w", entry, err)
		}
		allowed = append(allowed, network)
	}
	return &CIDRValidator{allowed: allowed}, nil
}

// Validate reports whether ipv4 may be stored as a monitoring target.
func (v *CIDRValidator) Validate(ipv4 string) error {
	ip := net.ParseIP(strings.TrimSpace(ipv4))
	if ip == nil {
		return ErrIPNotAllowed
	}
	ip = ip.To4()
	if ip == nil {
		return ErrIPNotAllowed
	}

	// Checked before the allowlist so 0.0.0.0/0 cannot re-admit these.
	if ip.IsUnspecified() || ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsMulticast() {
		return ErrIPNotAllowed
	}

	for _, network := range v.allowed {
		if network.Contains(ip) {
			return nil
		}
	}
	return ErrIPNotAllowed
}
