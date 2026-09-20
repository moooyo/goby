package config

import "testing"

func TestHTTPBindingDefaultsPreserveDeploymentAddressOwnership(t *testing.T) {
	for _, test := range []struct {
		address string
		want    HTTPBinding
	}{
		{"", HTTPBinding{}},
		{":8096", HTTPBinding{HttpPort: 8096}},
		{"127.0.0.1:0", HTTPBinding{BindHost: "127.0.0.1"}},
		{"media-host.internal:18096", HTTPBinding{BindHost: "media-host.internal", HttpPort: 18096}},
		{"[0:0:0:0:0:0:0:1]:65535", HTTPBinding{BindHost: "::1", HttpPort: 65535}},
		{"[::]:8096", HTTPBinding{BindHost: "::", HttpPort: 8096}},
	} {
		t.Run(test.address, func(t *testing.T) {
			got, err := HTTPBindingDefaults(test.address)
			if err != nil || got != test.want {
				t.Fatalf("deployment binding = %+v, error %v; want %+v", got, err, test.want)
			}
		})
	}
}

func TestHTTPBindingDefaultsRejectAmbiguousAddresses(t *testing.T) {
	for _, address := range []string{"http://127.0.0.1:8096", "127.0.0.1", ":http", ":-1", ":65536", ":08096", ":+8096",
		"[fe80::1%eth0]:8096", "127.0.0.1:8096/path", "host,other:8096", " host:8096", "host\t:8096", "host@:8096", "[invalid]:8096"} {
		t.Run(address, func(t *testing.T) {
			if _, err := HTTPBindingDefaults(address); err == nil {
				t.Fatal("ambiguous deployment binding was admitted")
			}
		})
	}
}
