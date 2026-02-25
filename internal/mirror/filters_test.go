package mirror

import (
	"net/url"
	"testing"
)

func TestShouldRejectURL_ExtensionForms(t *testing.T) {
	u := mustParseURL(t, "https://example.com/assets/leftfoot.gif")

	if !shouldRejectURL(u, []string{"gif"}) {
		t.Fatal("expected plain extension token to reject .gif")
	}
	if !shouldRejectURL(u, []string{".gif"}) {
		t.Fatal("expected dotted extension token to reject .gif")
	}
	if !shouldRejectURL(u, []string{"*.gif"}) {
		t.Fatal("expected glob extension token to reject .gif")
	}
}

func TestShouldRejectURL_DirectoryPattern(t *testing.T) {
	u := mustParseURL(t, "https://example.com/assets/leftfoot.gif")
	if !shouldRejectURL(u, []string{"assets/*"}) {
		t.Fatal("expected path glob to reject matching path")
	}
}

func TestShouldRejectURL_NoMatch(t *testing.T) {
	u := mustParseURL(t, "https://example.com/assets/image.png")
	if shouldRejectURL(u, []string{"gif"}) {
		t.Fatal("did not expect png to match gif reject pattern")
	}
}

func mustParseURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("parse url %q: %v", raw, err)
	}
	return u
}
