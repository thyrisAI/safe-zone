package unit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"thyris-sz/internal/handlers"
)

// newValidatorPatchRequest builds a PATCH /validators/{id} request with
// the path value set, the way the real ServeMux would do it.
func newValidatorPatchRequest(id, body string) *http.Request {
	req := httptest.NewRequest(http.MethodPatch, "/validators/"+id, strings.NewReader(body))
	req.SetPathValue("id", id)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestUpdateValidatorActive_RejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/validators/1", strings.NewReader(`{"is_active":false}`))
	req.SetPathValue("id", "1")
	rr := httptest.NewRecorder()

	handlers.UpdateValidatorActive(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for non-PATCH method, got %d", rr.Code)
	}
}

func TestUpdateValidatorActive_RejectsInvalidID(t *testing.T) {
	for _, id := range []string{"abc", "0", "-3"} {
		rr := httptest.NewRecorder()

		handlers.UpdateValidatorActive(rr, newValidatorPatchRequest(id, `{"is_active":false}`))

		if rr.Code != http.StatusBadRequest {
			t.Fatalf("expected 400 for id %q, got %d", id, rr.Code)
		}
	}
}

func TestUpdateValidatorActive_RejectsInvalidJSON(t *testing.T) {
	rr := httptest.NewRecorder()

	handlers.UpdateValidatorActive(rr, newValidatorPatchRequest("1", `{"is_active":`))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for malformed JSON, got %d", rr.Code)
	}
}

func TestUpdateValidatorActive_RequiresIsActiveField(t *testing.T) {
	rr := httptest.NewRecorder()

	handlers.UpdateValidatorActive(rr, newValidatorPatchRequest("1", `{}`))

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 when is_active is missing, got %d", rr.Code)
	}
	if !strings.Contains(rr.Body.String(), "is_active is required") {
		t.Fatalf("expected 'is_active is required' message, got %q", rr.Body.String())
	}
}
