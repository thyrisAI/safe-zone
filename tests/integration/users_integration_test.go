package integration

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// These tests need a running stack and dashboard admin credentials
// (TSZ_TEST_ADMIN_EMAIL and TSZ_TEST_ADMIN_PASSWORD). They create throwaway
// accounts with unique emails and always delete them again.

const testUserPassword = "ItDelete123!"

func uniqueTestEmail(prefix string) string {
	return fmt.Sprintf("%s-%d@example.com", prefix, time.Now().UnixNano())
}

func createTestUser(t *testing.T, admin *http.Client, email, role string) {
	t.Helper()

	resp, err := jsonRequest(admin, http.MethodPost, "/users",
		map[string]string{"email": email, "password": testUserPassword, "role": role})
	if err != nil {
		t.Fatalf("POST /users failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("POST /users returned %d, expected 201", resp.StatusCode)
	}
}

// findUserID looks a user up by email in GET /users.
func findUserID(t *testing.T, admin *http.Client, email string) (uint, bool) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, baseURL()+"/users", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	applyTestAuthHeaders(req)
	resp, err := admin.Do(req)
	if err != nil {
		t.Fatalf("GET /users failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /users returned %d", resp.StatusCode)
	}

	var users []struct {
		ID    uint   `json:"ID"`
		Email string `json:"email"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&users); err != nil {
		t.Fatalf("invalid users JSON: %v", err)
	}
	for _, u := range users {
		if u.Email == email {
			return u.ID, true
		}
	}
	return 0, false
}

func deleteUserStatus(t *testing.T, client *http.Client, id uint) int {
	t.Helper()

	req, err := http.NewRequest(http.MethodDelete, fmt.Sprintf("%s/users/%d", baseURL(), id), nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	applyTestAuthHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("DELETE /users/%d failed: %v", id, err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

// cleanupTestUser deletes the account if it still exists. Safe to defer.
func cleanupTestUser(t *testing.T, admin *http.Client, email string) {
	t.Helper()

	if id, ok := findUserID(t, admin, email); ok {
		if status := deleteUserStatus(t, admin, id); status != http.StatusNoContent {
			t.Errorf("cleanup: could not delete %s (status %d)", email, status)
		}
	}
}

// latestAuditRow returns the newest audit record with the given action.
func latestAuditRow(t *testing.T, client *http.Client, action string) (auditRow, bool) {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, baseURL()+"/audit/logs", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	applyTestAuthHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /audit/logs failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET /audit/logs returned %d", resp.StatusCode)
	}

	var rows []auditRow
	if err := json.NewDecoder(resp.Body).Decode(&rows); err != nil {
		t.Fatalf("invalid audit JSON: %v", err)
	}
	for _, row := range rows { // newest first
		if row.Action == action {
			return row, true
		}
	}
	return auditRow{}, false
}

func TestUsers_DeleteClosesSessionsAndFreesTheEmail(t *testing.T) {
	adminEmail, adminPassword := adminCredentials(t)
	admin := sessionLogin(t, adminEmail, adminPassword)

	email := uniqueTestEmail("it-delete")
	createTestUser(t, admin, email, "viewer")
	defer cleanupTestUser(t, admin, email)

	victim := sessionLogin(t, email, testUserPassword)
	if got := meStatus(t, victim); got != http.StatusOK {
		t.Fatalf("before the delete the user's session should work, got %d", got)
	}

	id, ok := findUserID(t, admin, email)
	if !ok {
		t.Fatalf("the new user %s is missing from GET /users", email)
	}
	if status := deleteUserStatus(t, admin, id); status != http.StatusNoContent {
		t.Fatalf("DELETE returned %d, expected 204", status)
	}

	if got := meStatus(t, victim); got != http.StatusUnauthorized {
		t.Errorf("the deleted user's session must be closed (401), got %d", got)
	}
	if _, err := tryLogin(email, testUserPassword); err == nil {
		t.Error("the deleted user must not be able to sign in again")
	}
	if _, stillThere := findUserID(t, admin, email); stillThere {
		t.Error("the deleted user is still listed in GET /users")
	}

	// The audit record keeps the email, so the history stays readable.
	row, found := latestAuditRow(t, admin, "user_deleted")
	if !found {
		t.Fatal("no user_deleted record in the audit log")
	}
	if row.Status != "success" || !strings.Contains(row.Details, email) {
		t.Errorf("unexpected audit record: %+v", row)
	}

	// A hard delete frees the email: the same address can be used again.
	createTestUser(t, admin, email, "viewer")
}

func TestUsers_AdminCannotDeleteOwnAccount(t *testing.T) {
	adminEmail, adminPassword := adminCredentials(t)
	admin := sessionLogin(t, adminEmail, adminPassword)

	id, ok := findUserID(t, admin, strings.ToLower(adminEmail))
	if !ok {
		t.Fatalf("admin %s is missing from GET /users", adminEmail)
	}
	if status := deleteUserStatus(t, admin, id); status != http.StatusBadRequest {
		t.Fatalf("deleting your own account returned %d, expected 400", status)
	}
	if got := meStatus(t, admin); got != http.StatusOK {
		t.Errorf("the admin's session must still work, got %d", got)
	}
}

func TestUsers_DeleteUnknownUserReturnsNotFound(t *testing.T) {
	adminEmail, adminPassword := adminCredentials(t)
	admin := sessionLogin(t, adminEmail, adminPassword)

	if status := deleteUserStatus(t, admin, 999999999); status != http.StatusNotFound {
		t.Errorf("deleting an unknown user returned %d, expected 404", status)
	}
}

func TestUsers_ViewerCannotDeleteUsers(t *testing.T) {
	adminEmail, adminPassword := adminCredentials(t)
	admin := sessionLogin(t, adminEmail, adminPassword)

	viewerEmail := uniqueTestEmail("it-viewer")
	createTestUser(t, admin, viewerEmail, "viewer")
	defer cleanupTestUser(t, admin, viewerEmail)

	targetEmail := uniqueTestEmail("it-target")
	createTestUser(t, admin, targetEmail, "viewer")
	defer cleanupTestUser(t, admin, targetEmail)

	targetID, ok := findUserID(t, admin, targetEmail)
	if !ok {
		t.Fatalf("the target user %s is missing from GET /users", targetEmail)
	}

	viewer := sessionLogin(t, viewerEmail, testUserPassword)
	if status := deleteUserStatus(t, viewer, targetID); status != http.StatusForbidden {
		t.Errorf("a viewer deleting a user returned %d, expected 403", status)
	}
	if _, stillThere := findUserID(t, admin, targetEmail); !stillThere {
		t.Error("the target user must still exist after a forbidden delete")
	}
}
