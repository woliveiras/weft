package domain

import (
	"fmt"
	"net"
	"strings"
)

// Egress hosts are exact canonical ASCII DNS names, never URLs, IPs or patterns.
func ValidateEgressHosts(hosts []string) error {
	if len(hosts) > 64 {
		return fmt.Errorf("at most 64 egress hosts are allowed")
	}
	seen := map[string]bool{}
	for _, host := range hosts {
		if len(host) > 253 || !strings.Contains(host, ".") || host != strings.ToLower(host) || net.ParseIP(host) != nil || seen[host] {
			return fmt.Errorf("egress hosts must be unique lowercase DNS names, without IPs, ports or wildcards")
		}
		for _, label := range strings.Split(host, ".") {
			if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
				return fmt.Errorf("invalid egress DNS label")
			}
			for _, c := range label {
				if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
					return fmt.Errorf("egress hosts must be ASCII DNS names")
				}
			}
		}
		seen[host] = true
	}
	return nil
}
