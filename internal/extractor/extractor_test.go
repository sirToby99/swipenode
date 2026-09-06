package extractor

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
)

func TestConditionalValidatorSanitization(t *testing.T) {
	if got := cleanHTTPValidator(` "v1" `); got != `"v1"` {
		t.Fatalf("validator = %q", got)
	}
	if got := cleanHTTPValidator("value\r\nInjected: yes"); got != "" {
		t.Fatalf("header injection retained: %q", got)
	}
}

func TestValidateURLRejectsNonPublicAddresses(t *testing.T) {
	tests := []string{
		"http://127.0.0.1/",
		"http://10.0.0.1/",
		"http://169.254.169.254/",
		"http://198.18.0.1/",
		"http://192.0.2.1/",
		"http://[::1]/",
		"http://[2001:db8::1]/",
		"http://[fe80::1]/",
		"http://[::]/",
	}
	for _, target := range tests {
		t.Run(target, func(t *testing.T) {
			_, _, err := validateURL(context.Background(), target)
			if !errors.Is(err, ErrSSRFBlocked) {
				t.Fatalf("expected SSRF block, got %v", err)
			}
		})
	}
}

func TestValidateURLAcceptsPublicIPAddress(t *testing.T) {
	_, _, err := validateURL(context.Background(), "https://93.184.216.34/")
	if err != nil {
		t.Fatalf("expected public address to validate: %v", err)
	}
}

func TestParseHTMLExtractsStructuredTechnicalDocument(t *testing.T) {
	fixture, err := os.Open("testdata/technical-document.html")
	if err != nil {
		t.Fatal(err)
	}
	defer fixture.Close()

	title, content := parseHTML(fixture)
	if title != "Example Transport Specification" {
		t.Fatalf("title=%q", title)
	}
	for _, expected := range []string{
		"2.3 Port Number",
		"When EXAMPLE/TLS is run over TCP/IP, the default port is 443.",
		"Implementations may use another reliable transport.",
		"client.connect( port=443 )",
	} {
		if !strings.Contains(content, expected) {
			t.Fatalf("content does not contain %q:\n%s", expected, content)
		}
	}
	for _, excluded := range []string{"navigation noise", "footer noise", "script noise"} {
		if strings.Contains(content, excluded) {
			t.Fatalf("content retained excluded page chrome %q", excluded)
		}
	}
}
