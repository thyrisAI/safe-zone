package unit

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"thyris-sz/internal/handlers"
)

func TestListAuditLogs_RejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/audit/logs", nil)
	rr := httptest.NewRecorder()
	handlers.ListAuditLogs(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST /audit/logs, got %d", rr.Code)
	}
}

// The filter is validated before the database is touched, so these cases
// need no DB.
func TestListAuditLogs_RejectsInvalidFilters(t *testing.T) {
	cases := map[string]string{
		"non-numeric user_id": "/audit/logs?user_id=abc",
		"negative user_id":    "/audit/logs?user_id=-1",
		"sql-ish user_id":     "/audit/logs?user_id=1%20OR%201=1",
		"unknown status":      "/audit/logs?status=maybe",
	}

	for name, target := range cases {
		t.Run(name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			rr := httptest.NewRecorder()
			handlers.ListAuditLogs(rr, req)

			if rr.Code != http.StatusBadRequest {
				t.Fatalf("expected 400 for %s, got %d", target, rr.Code)
			}
		})
	}
}

func TestMyActivity_RejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/auth/me/activity", nil)
	rr := httptest.NewRecorder()
	handlers.MyActivity(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for POST /auth/me/activity, got %d", rr.Code)
	}
}

// Without a session in the request context the handler must refuse
// before touching the database.
func TestMyActivity_RequiresSession(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/auth/me/activity", nil)
	rr := httptest.NewRecorder()
	handlers.MyActivity(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 without a session, got %d", rr.Code)
	}
}
