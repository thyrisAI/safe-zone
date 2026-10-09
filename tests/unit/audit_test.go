package unit

import (
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"

	"thyris-sz/internal/audit"
)

func TestTruncate_ShortStringUnchanged(t *testing.T) {
	if got := audit.Truncate("admin@example.com", 254); got != "admin@example.com" {
		t.Fatalf("expected unchanged string, got %q", got)
	}
}

func TestTruncate_ExactLengthUnchanged(t *testing.T) {
	in := strings.Repeat("a", 254)
	if got := audit.Truncate(in, 254); got != in {
		t.Fatalf("string at the limit must not be cut, got length %d", len(got))
	}
}

func TestTruncate_LongStringIsCut(t *testing.T) {
	in := strings.Repeat("a", 1000)
	got := audit.Truncate(in, 254)
	if len(got) != 254 {
		t.Fatalf("expected length 254, got %d", len(got))
	}
}

func TestTruncate_CountsCharactersNotBytes(t *testing.T) {
	// Each of these Turkish letters is 2 bytes in UTF-8. Cutting by bytes
	// could split a letter in half and produce invalid text.
	got := audit.Truncate("çşğüö", 3)
	if got != "çşğ" {
		t.Fatalf("expected %q, got %q", "çşğ", got)
	}
	if !utf8.ValidString(got) {
		t.Fatalf("result is not valid UTF-8: %q", got)
	}
}

func TestClientIP_StripsPortFromIPv4(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.5:54321"
	if got := audit.ClientIP(req); got != "192.168.1.5" {
		t.Fatalf("expected 192.168.1.5, got %q", got)
	}
}

func TestClientIP_StripsPortFromIPv6(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "[::1]:8080"
	if got := audit.ClientIP(req); got != "::1" {
		t.Fatalf("expected ::1, got %q", got)
	}
}

func TestClientIP_AddressWithoutPort(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1"
	if got := audit.ClientIP(req); got != "10.0.0.1" {
		t.Fatalf("expected 10.0.0.1, got %q", got)
	}
}

func TestClientIP_IgnoresForwardedForHeader(t *testing.T) {
	// X-Forwarded-For is client-controlled; trusting it would let anyone
	// write a fake IP into the audit log.
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "192.168.1.5:54321"
	req.Header.Set("X-Forwarded-For", "6.6.6.6")
	if got := audit.ClientIP(req); got != "192.168.1.5" {
		t.Fatalf("header must be ignored, got %q", got)
	}
}

func TestClientIP_OverlongValueIsCapped(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = strings.Repeat("x", 200)
	if got := audit.ClientIP(req); len(got) > 45 {
		t.Fatalf("expected at most 45 chars, got %d", len(got))
	}
}