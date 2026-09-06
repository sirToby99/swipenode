package verification

import (
	"errors"
	"strings"
	"testing"
)

func TestSanitizeFetchURLBlocksSSRFTargetsAndCredentials(t *testing.T) {
	blocked := []string{
		"http://localhost/admin", "http://service.local/admin", "http://127.0.0.1/",
		"http://169.254.169.254/latest/meta-data/", "http://10.0.0.1/", "http://172.16.0.1/",
		"http://192.168.1.1/", "http://[::1]/", "http://[fe80::1]/",
		"http://metadata.google.internal/computeMetadata/v1/", "https://user:password@example.com/",
		"ftp://example.com/file",
	}
	for _, raw := range blocked {
		if safe, err := SanitizeFetchURL(raw); !errors.Is(err, ErrUnsafeURL) {
			t.Errorf("SanitizeFetchURL(%q)=(%q, %v), want ErrUnsafeURL", raw, safe, err)
		}
	}
}

func TestSanitizeFetchURLStripsSensitiveQueryValues(t *testing.T) {
	raw := "https://docs.github.com/api?token=top-secret&api-key=also-secret&X-Amz-Credential=credential&signature=signed&page=2#private"
	safe, err := SanitizeFetchURL(raw)
	if err != nil {
		t.Fatal(err)
	}
	if safe != "https://docs.github.com/api?page=2" || strings.Contains(safe, "secret") || strings.Contains(safe, "signed") {
		t.Fatalf("unsafe sanitized URL: %q", safe)
	}
}

func TestAuthorityPolicyMatchesReviewedDomainsConservatively(t *testing.T) {
	tests := []struct {
		name string
		url  string
		want string
	}{
		{name: "documented official subdomain", url: "https://docs.nvidia.com/jetson/", want: "authoritative"},
		{name: "unreviewed nested subdomain", url: "https://developer.docs.nvidia.com/guide", want: "unknown"},
		{name: "unrelated domain", url: "https://example.com/jetson/", want: "unknown"},
		{name: "deceptive suffix", url: "https://nvidia.com.evil.example/jetson/", want: "unknown"},
		{name: "missing domain boundary", url: "https://notnvidia.com/jetson/", want: "unknown"},
		{name: "insecure transport", url: "http://docs.nvidia.com/jetson/", want: "unknown"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := authorityForURL(test.url); got != test.want {
				t.Fatalf("authorityForURL(%q)=%q, want %q", test.url, got, test.want)
			}
		})
	}
}

func TestSourceHostMatchesDomainHonorsSubdomainPolicy(t *testing.T) {
	rootOnly := sourceAuthorityDomain{Domain: "docs.example.com"}
	withSubdomains := sourceAuthorityDomain{Domain: "example.com", IncludeSubdomains: true}

	if sourceHostMatchesDomain("api.docs.example.com", rootOnly) {
		t.Fatal("exact-only policy accepted an unreviewed subdomain")
	}
	if !sourceHostMatchesDomain("api.docs.example.com", withSubdomains) {
		t.Fatal("subdomain policy rejected a dot-delimited subdomain")
	}
	if sourceHostMatchesDomain("badexample.com", withSubdomains) {
		t.Fatal("subdomain policy accepted a hostname without a domain boundary")
	}
}
