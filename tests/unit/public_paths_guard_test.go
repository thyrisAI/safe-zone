package unit

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

// AUTH_PUBLIC_PATHS makes the outer token check step aside for these path
// groups, so a signed-in dashboard user does not need an API token. The real
// decision then belongs to each route's own middleware. If a route under one
// of these paths had no guard of its own, it would be open to everyone.
//
// Keep this list in step with the default AUTH_PUBLIC_PATHS in
// internal/config/config.go and in .env.example. /auth/* is left out on
// purpose: sign-in has to be reachable without a session.
var guardedPathGroups = []string{
	"/patterns",
	"/allowlist",
	"/blacklist",
	"/validators",
	"/users",
	"/dashboard",
	"/audit",
}

var routeGuards = []string{
	"middleware.RequireSession(",
	"middleware.RequireAdminSession(",
	"middleware.RequireReadAccess(",
	"middleware.RequireWriteAccess(",
}

// routeStart finds the start of a route registration and captures its
// method and path, for example: mux.Handle("GET /users", ...
var routeStart = regexp.MustCompile(`mux\.Handle(?:Func)?\(\s*"([A-Z]+) (/[^"]*)"`)

func isUnderGroup(path, group string) bool {
	return path == group || strings.HasPrefix(path, group+"/")
}

func TestPublicPaths_EveryRouteUnderThemHasItsOwnGuard(t *testing.T) {
	source, err := os.ReadFile("../../main.go")
	if err != nil {
		t.Fatalf("cannot read main.go: %v", err)
	}
	text := string(source)

	matches := routeStart.FindAllStringSubmatchIndex(text, -1)
	if len(matches) == 0 {
		t.Fatal("found no routes in main.go; the pattern in this test needs updating")
	}

	checked := map[string]int{}

	for i, m := range matches {
		// A registration runs until the next one or the next blank line.
		end := len(text)
		if i+1 < len(matches) {
			end = matches[i+1][0]
		}
		if blank := strings.Index(text[m[0]:end], "\n\n"); blank >= 0 {
			end = m[0] + blank
		}
		statement := text[m[0]:end]

		method := text[m[2]:m[3]]
		path := text[m[4]:m[5]]

		for _, group := range guardedPathGroups {
			if !isUnderGroup(path, group) {
				continue
			}
			checked[group]++

			guarded := false
			for _, guard := range routeGuards {
				if strings.Contains(statement, guard) {
					guarded = true
					break
				}
			}
			if !guarded {
				t.Errorf("%s %s is under the public path %s but has no session or token guard of its own (%s). "+
					"Add one of the middleware guards in main.go.",
					method, path, group, strings.Join(routeGuards, ", "))
			}
		}
	}

	// A broken pattern must not let this test pass without checking anything.
	for _, group := range guardedPathGroups {
		if checked[group] == 0 {
			t.Errorf("no route under %s was found in main.go; update guardedPathGroups or the route pattern", group)
		}
	}
}
