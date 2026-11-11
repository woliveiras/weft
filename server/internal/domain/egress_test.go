package domain

import "testing"

func TestEgressHosts(t *testing.T) {
	for _, hosts := range [][]string{{"example.com"}, {"registry.npmjs.org", "xn--bcher-kva.example"}, nil} {
		if err := ValidateEgressHosts(hosts); err != nil {
			t.Fatal(err)
		}
	}
	for _, hosts := range [][]string{{"*.example.com"}, {"Example.com"}, {"example.com."}, {"example.com:443"}, {"https://example.com"}, {"127.0.0.1"}, {"::1"}, {"local"}, {"a..com"}, {"-a.com"}, {"é.com"}, {"example.com", "example.com"}, make([]string, 65)} {
		if ValidateEgressHosts(hosts) == nil {
			t.Fatal("accepted", hosts)
		}
	}
}
