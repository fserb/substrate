package substrate

import (
	"net"
)

var privateIPBlocks []*net.IPNet

func init() {
	// Initialize private IP blocks once at startup
	for _, cidr := range []string{
		"127.0.0.0/8",    // IPv4 loopback
		"10.0.0.0/8",     // RFC1918
		"172.16.0.0/12",  // RFC1918
		"192.168.0.0/16", // RFC1918
		"169.254.0.0/16", // RFC3927 link-local
		"100.64.0.0/10",  // RFC6598 CGNAT (Tailscale)
		"::1/128",        // IPv6 loopback
		"fe80::/10",      // IPv6 link-local
		"fc00::/7",       // IPv6 unique local addr
	} {
		_, block, err := net.ParseCIDR(cidr)
		if err != nil {
			panic(err)
		}
		privateIPBlocks = append(privateIPBlocks, block)
	}
}

// isInternalIP checks if the given IP address is internal/private
func isInternalIP(remoteAddr string) bool {
	// Extract IP from "IP:port" format
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		// If splitting fails, assume the whole string is an IP
		host = remoteAddr
	}

	ip := net.ParseIP(host)
	if ip == nil {
		// If IP parsing fails, assume it's external for security
		return false
	}

	// Check for loopback and special addresses
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() {
		return true
	}

	// Check against private IP blocks
	for _, block := range privateIPBlocks {
		if block.Contains(ip) {
			return true
		}
	}

	return false
}
