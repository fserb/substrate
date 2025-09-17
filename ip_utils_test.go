package substrate

import "testing"

func TestIsInternalIP(t *testing.T) {
	tests := []struct {
		name     string
		addr     string
		expected bool
	}{
		// Internal IPs
		{"loopback IPv4", "127.0.0.1:8080", true},
		{"loopback IPv4 no port", "127.0.0.1", true},
		{"loopback IPv6", "[::1]:8080", true},
		{"RFC1918 10.x", "10.0.0.1:9000", true},
		{"RFC1918 172.16.x", "172.16.0.1:9000", true},
		{"RFC1918 192.168.x", "192.168.1.1:9000", true},
		{"link-local IPv4", "169.254.1.1:8080", true},
		{"CGNAT Tailscale", "100.64.1.1:8080", true},
		{"link-local IPv6", "[fe80::1]:8080", true},
		{"unique local IPv6", "[fc00::1]:8080", true},

		// External IPs
		{"public IPv4", "8.8.8.8:80", false},
		{"public IPv4 no port", "8.8.8.8", false},
		{"public IPv6", "[2001:db8::1]:80", false},
		{"invalid IP", "not-an-ip", false},
		{"empty string", "", false},

		// Edge cases
		{"172.15.x (not RFC1918)", "172.15.255.255:8080", false},
		{"172.32.x (not RFC1918)", "172.32.0.1:8080", false},
		{"11.x (not RFC1918)", "11.0.0.1:8080", false},
		{"192.169.x (not RFC1918)", "192.169.1.1:8080", false},
		{"100.63.x (not CGNAT)", "100.63.255.255:8080", false},
		{"100.128.x (not CGNAT)", "100.128.0.1:8080", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isInternalIP(tt.addr)
			if result != tt.expected {
				t.Errorf("isInternalIP(%q) = %v, want %v", tt.addr, result, tt.expected)
			}
		})
	}
}
