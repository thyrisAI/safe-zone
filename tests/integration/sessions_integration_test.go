package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"
)

// This test needs a running stack and dashboard credentials
// (TSZ_TEST_ADMIN_EMAIL and TSZ_TEST_ADMIN_PASSWORD). It changes the admin
// password for a moment and always puts the original one back.

func adminCredentials(t *testing.T) (string, string) {
	t.Helper()

	email := strings.TrimSpace(os.Getenv("TSZ_TEST_ADMIN_EMAIL"))
	password := os.Getenv("TSZ_TEST_ADMIN_PASSWORD")
	if email == "" || password == "" {
		t.Skip("TSZ_TEST_ADMIN_EMAIL / TSZ_TEST_ADMIN_PASSWORD not set; skipping session test")
	}
	return email, password
}

func jsonRequest(client *http.Client, method, path string, body interface{}) (*http.Response, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequest(method, baseURL()+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	applyTestAuthHeaders(req)
	return client.Do(req)
}

// tryLogin signs in and returns a client that holds its own session cookie.
func tryLogin(email, password string) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}
	client := &http.Client{Jar: jar, Timeout: 15 * time.Second}

	resp, err := jsonRequest(client, http.MethodPost, "/auth/login",
		map[string]string{"email": email, "password": password})
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("login returned %d", resp.StatusCode)
	}
	return client, nil
}

func sessionLogin(t *testing.T, email, password string) *http.Client {
	t.Helper()
	client, err := tryLogin(email, password)
	if err != nil {
		t.Fatalf("sign-in failed: %v", err)
	}
	return client
}

func meStatus(t *testing.T, client *http.Client) int {
	t.Helper()

	req, err := http.NewRequest(http.MethodGet, baseURL()+"/auth/me", nil)
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	applyTestAuthHeaders(req)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("GET /auth/me failed: %v", err)
	}
	defer resp.Body.Close()
	return resp.StatusCode
}

func postPassword(client *http.Client, current, next string) (int, error) {
	resp, err := jsonRequest(client, http.MethodPost, "/auth/me/password",
		map[string]string{"current_password": current, "new_password": next})
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return resp.StatusCode, nil
}

type auditRow struct {
	Action  string `json:"action"`
	Status  string `json:"status"`
	Details string `json:"details"`
}

func latestPasswordChange(t *testing.T, client *http.Client) auditRow {
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
		if row.Action == "password_change" && row.Status == "success" {
			return row
		}
	}
	t.Fatal("no successful password_change record in the audit log")
	return auditRow{}
}

func TestSessions_PasswordChangeClosesTheOtherSessions(t *testing.T) {
	email, password := adminCredentials(t)
	tempPassword := password + "_it1"

	keep := sessionLogin(t, email, password)  // the session that changes the password
	other := sessionLogin(t, email, password) // another browser or device

	if got := meStatus(t, keep); got != http.StatusOK {
		t.Fatalf("before the change the first session should work, got %d", got)
	}
	if got := meStatus(t, other); got != http.StatusOK {
		t.Fatalf("before the change the second session should work, got %d", got)
	}

	status, err := postPassword(keep, password, tempPassword)
	if err != nil {
		t.Fatalf("password change failed: %v", err)
	}
	if status != http.StatusNoContent {
		t.Fatalf("password change returned %d, expected 204", status)
	}

	// Whatever happens next, put the original password back.
	defer func() {
		restore, err := tryLogin(email, tempPassword)
		if err != nil {
			t.Errorf("could not sign in to restore the password (it is still the original plus the suffix _it1): %v", err)
			return
		}
		if status, err := postPassword(restore, tempPassword, password); err != nil || status != http.StatusNoContent {
			t.Errorf("could not restore the password (status %d, err %v); it is still the original plus the suffix _it1", status, err)
		}
	}()

	if got := meStatus(t, keep); got != http.StatusOK {
		t.Errorf("the session that changed the password must stay open, got %d", got)
	}
	if got := meStatus(t, other); got != http.StatusUnauthorized {
		t.Errorf("the other session must be closed (401), got %d", got)
	}

	// The audit record says how many sessions were closed: at least ours.
	details := latestPasswordChange(t, keep).Details
	if !strings.HasSuffix(details, " other sessions closed") {
		t.Fatalf("unexpected audit details %q", details)
	}
	closed, err := strconv.Atoi(strings.TrimSuffix(details, " other sessions closed"))
	if err != nil || closed < 1 {
		t.Errorf("audit details %q should report at least 1 closed session", details)
	}
}
