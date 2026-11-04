package mynet

import "testing"

// TestMdnsQueryName covers #462: LookupHost's mDNS fallback used to build its
// query name with a bare `host + ".local"` concatenation, which produces a
// malformed double-suffixed name (e.g. "filtration-hiver.local..local") when
// host already ends in ".local" and/or carries a trailing dot — the canonical
// FQDN form many mDNS/zeroconf libraries hand back (see
// myhome/daemon/watch/zeroconf.go's entry.HostName). A malformed query never
// gets a response, burning the full mDNS timeout before failing.
func TestMdnsQueryName(t *testing.T) {
	cases := []struct {
		name string
		host string
		want string
	}{
		{"bare hostname", "filtration-hiver", "filtration-hiver.local"},
		{"trailing-dot FQDN", "filtration-hiver.local.", "filtration-hiver.local"},
		{"already .local suffixed", "filtration-hiver.local", "filtration-hiver.local"},
		{"trailing dot without .local", "filtration-hiver.", "filtration-hiver.local"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mdnsQueryName(tc.host)
			if got != tc.want {
				t.Errorf("mdnsQueryName(%q) = %q, want %q", tc.host, got, tc.want)
			}
		})
	}
}
