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