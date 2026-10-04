package unit

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"thyris-sz/internal/handlers"
	"thyris-sz/internal/middleware"
)

func TestCreateUser_RejectsWrongMethod(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/users", nil)
	rr := httptest.NewRecorder()
	handlers.CreateUser(rr, req)

	if rr.Code != http.StatusMethodNotAllowed {
		t.Fatalf("expected 405 for GET /users, got %d", rr.Code)
	}
}

func TestCreateUser_RejectsInvalidJSON(t *testing.T) {
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader("not json"))
	rr := httptest.NewRecorder()
	handlers.CreateUser(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid JSON, got %d", rr.Code)
	}
}

func TestCreateUser_RejectsMissingEmail(t *testing.T) {
	body := `{"password":"test1234","role":"viewer"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handlers.CreateUser(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing email, got %d", rr.Code)
	}
}

func TestCreateUser_RejectsMissingPassword(t *testing.T) {
	body := `{"email":"nobody@example.com","role":"viewer"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handlers.CreateUser(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing password, got %d", rr.Code)
	}
}

func TestCreateUser_RejectsInvalidRole(t *testing.T) {
	body := `{"email":"nobody@example.com","password":"test1234","role":"superadmin"}`
	req := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(body))
	rr := httptest.NewRecorder()
	handlers.CreateUser(rr, req)

	if rr.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for invalid role, got %d", rr.Code)
	}
}

func TestRequireAdminSession_RejectsNoCookie(t *testing.T) {
	h := middleware.RequireAdminSession(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodPost, "/users", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	if rr.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 with no session cookie, got %d", rr.Code)
	}
}