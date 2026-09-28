package unit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"thyris-sz/internal/handlers"
)

// newPatchRequest builds a PATCH /patterns/{id} request with the path
// value set, the way the real ServeMux would do it.
func newPatchRequest(id, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPatch, "/patterns/"+id, strings.NewReader(body))
	req.SetPathValue("id", id)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestUpdatePatternActive_RejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/patterns/1", strings.NewReader(`{"is_active":false}`))
	req.SetPathValue("id", "1")
	rr := httptest.NewRecorder()

	handlers.UpdatePatternActive(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for non-PATCH method, got %d", rr.Code)
	}
}

func TestUpdatePatternActive_RejectsInvalidID(t *testing.T) {
	rr := httptest.NewRecorder()

	handlers.UpdatePatternActive(rr, newPatchRequest("abc", `{"is_active":false}`))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for non-numeric id, got %d", rr.Code)
	}
}

func TestUpdatePatternActive_RejectsInvalidJSON(t *testing.T) {
	rr := httptest.NewRecorder()

	handlers.UpdatePatternActive(rr, newPatchRequest("1", `{"is_active":`))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rr.Code)
	}
}

func TestUpdatePatternActive_RequiresIsActiveField(t *testing.T) {
	rr := httptest.NewRecorder()

	handlers.UpdatePatternActive(rr, newPatchRequest("1", `{}`))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when is_active is missing, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "is_active is required") {
		t.Fatalf("expected 'is_active is required' message, got %q", rr.Body.String())
	}
}