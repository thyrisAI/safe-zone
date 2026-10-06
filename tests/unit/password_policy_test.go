package unit

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"thyris-sz/internal/auth"
	"thyris-sz/internal/handlers"
)

func TestValidateNewPassword(t *testing.T) {
	cases := []struct {
		name    string
		current string
		next    string
		want    error
	}{
		{"valid", "old-password", "new-password-1", nil},
		{"exactly minimum length", "old-password", "12345678", nil},
		{"too short", "old-password", "1234567", auth.ErrPasswordTooShort},
		{"empty", "old-password", "", auth.ErrPasswordTooShort},
		{"counts characters not bytes", "old-password", "\u00e7\u00e7\u00e7\u00e7\u00e7\u00e7\u00e7", auth.ErrPasswordTooShort},
		{"eight multibyte characters", "old-password", "\u00e7\u00e7\u00e7\u00e7\u00e7\u00e7\u00e7\u00e7", nil},
		{"exactly maximum length", "old-password", strings.Repeat("a", auth.MaxPasswordLength), nil},
		{"too long", "old-password", strings.Repeat("a", auth.MaxPasswordLength+1), auth.ErrPasswordTooLong},
		{"same as current", "same-password", "same-password", auth.ErrPasswordUnchanged},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := auth.ValidateNewPassword(tc.current, tc.next)
			if !errors.Is(got, tc.want) {
				t.Fatalf("expected %v, got %v", tc.want, got)
			}
		})
	}
}

func TestChangePassword_RejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/auth/me/password", nil)
	rr := httptest.NewRecorder()
	handlers.ChangePassword(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405, got %d", rr.Code)
	}
}

// Without a session in the request context the handler must refuse before
// reading the body or touching the database.
func TestChangePassword_RequiresSession(t *testing.T) {
	body := strings.NewReader(`{"current_password":"a","new_password":"abcdefgh"}`)
	req := httptest.NewRequest(http.MethodPost, "/auth/me/password", body)
	rr := httptest.NewRecorder()
	handlers.ChangePassword(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", rr.Code)
	}
}
