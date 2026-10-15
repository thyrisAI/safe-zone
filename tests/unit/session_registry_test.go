package unit

import (
	"strings"
	"testing"

	"thyris-sz/internal/auth"
)

func TestUserSessionsKey_IsPerUser(t *testing.T) {
	if auth.UserSessionsKey(7) == auth.UserSessionsKey(8) {
		t.Fatal("different users must have different registers")
	}
	if got := auth.UserSessionsKey(7); got != "user_sessions:7" {
		t.Fatalf("unexpected key %q", got)
	}
}

func TestUserSessionsKey_NeverLooksLikeASessionKey(t *testing.T) {
	// A register must never be readable as a session, or the middleware
	// could treat a set of ids as a login.
	if strings.HasPrefix(auth.UserSessionsKey(1), "session:") {
		t.Fatal("register key must not start with the session prefix")
	}
}

func TestOtherSessionIDs_DropsOnlyTheKeptSession(t *testing.T) {
	got := auth.OtherSessionIDs([]string{"abc", "def", "xyz"}, "abc")
	want := []string{"def", "xyz"}

	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestOtherSessionIDs_KeptSessionNotInTheListChangesNothing(t *testing.T) {
	got := auth.OtherSessionIDs([]string{"def", "xyz"}, "abc")
	if len(got) != 2 {
		t.Fatalf("expected both sessions to be revoked, got %v", got)
	}
}

func TestOtherSessionIDs_OnlyTheKeptSessionLeavesNothing(t *testing.T) {
	if got := auth.OtherSessionIDs([]string{"abc"}, "abc"); len(got) != 0 {
		t.Fatalf("expected nothing to revoke, got %v", got)
	}
	if got := auth.OtherSessionIDs(nil, "abc"); len(got) != 0 {
		t.Fatalf("expected nothing to revoke, got %v", got)
	}
}

func TestOtherSessionIDs_EmptyKeepIDRevokesEverything(t *testing.T) {
	if got := auth.OtherSessionIDs([]string{"abc", "def"}, ""); len(got) != 2 {
		t.Fatalf("expected everything to be revoked, got %v", got)
	}
}
