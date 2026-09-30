package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"
)

type contextKey string

const principalContextKey contextKey = "auth_principal"

type Principal struct {
	Token       string
	Permissions map[string]bool
}

type AuthConfig struct {
	Enabled            bool
	TokenPermissions   string // CSV: token=perm1|perm2|perm3
	AdminAPIKey        string
	PublicPaths        string // CSV: /healthz,/ready
	RequireBearerToken bool
}

var cfg = AuthConfig{
	Enabled:     false,
	PublicPaths: "/healthz,/ready",
}

var tokenPermissions = map[string]map[string]bool{}
var publicPaths = map[string]bool{}

func Init(conf AuthConfig) {
	cfg = conf
	tokenPermissions = parseTokenPermissions(conf.TokenPermissions)
	publicPaths = parsePublicPaths(conf.PublicPaths)
}

func Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !cfg.Enabled || isPublicPath(r.URL.Path) {
			next.ServeHTTP(w, r)
			return
		}

		principal, err := AuthenticateRequest(r)
		if err != nil {
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}

		ctx := context.WithValue(r.Context(), principalContextKey, principal)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func RequirePermission(permission string) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if !cfg.Enabled {
				next.ServeHTTP(w, r)
				return
			}

			principal, ok := FromContext(r.Context())
			if !ok {
				http.Error(w, "Unauthorized", http.StatusUnauthorized)
				return
			}

			if !hasPermission(principal, permission) {
				http.Error(w, "Forbidden", http.StatusForbidden)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

func AuthenticateRequest(r *http.Request) (*Principal, error) {
	token, err := extractToken(r)
	if err != nil {
		return nil, err
	}

	if cfg.AdminAPIKey != "" && secureEqual(token, cfg.AdminAPIKey) {
		return &Principal{Token: token, Permissions: map[string]bool{"*": true}}, nil
	}

	for storedToken, perms := range tokenPermissions {
		if secureEqual(token, storedToken) {
			return &Principal{Token: token, Permissions: perms}, nil
		}
	}

	return nil, errors.New("invalid token")
}

func FromContext(ctx context.Context) (*Principal, bool) {
	v := ctx.Value(principalContextKey)
	if v == nil {
		return nil, false
	}
	p, ok := v.(*Principal)
	return p, ok
}

func HasPermission(r *http.Request, permission string) bool {
	if !cfg.Enabled {
		return true
	}
	principal, ok := FromContext(r.Context())
	if !ok {
		return false
	}
	return hasPermission(principal, permission)
}

// CheckTokenPermission authenticates the request directly from its headers,
// independent of any context value set by the outer Middleware. Routes that
// are marked public at the Middleware level (so that a session-only request
// can reach them) but still need to accept CLI/SDK tokens use this instead
// of HasPermission, which depends on context the outer Middleware no longer
// sets for these paths.
func CheckTokenPermission(r *http.Request, permission string) bool {
	if !cfg.Enabled {
		return true
	}
	principal, err := AuthenticateRequest(r)
	if err != nil {
		return false
	}
	return hasPermission(principal, permission)
}

func hasPermission(p *Principal, permission string) bool {
	if p == nil {
		return false
	}
	if p.Permissions["*"] {
		return true
	}
	return p.Permissions[permission]
}

func extractToken(r *http.Request) (string, error) {
	authHeader := strings.TrimSpace(r.Header.Get("Authorization"))
	hasBearer := strings.HasPrefix(strings.ToLower(authHeader), "bearer ")
	if authHeader != "" {
		if hasBearer {
			token := strings.TrimSpace(authHeader[7:])
			if token != "" {
				return token, nil
			}
		}
	}

	if apiKey := strings.TrimSpace(r.Header.Get("X-API-Key")); apiKey != "" {
		return apiKey, nil
	}
	if adminKey := strings.TrimSpace(r.Header.Get("X-ADMIN-KEY")); adminKey != "" {
		return adminKey, nil
	}

	if cfg.RequireBearerToken {
		if authHeader != "" && !hasBearer {
			return "", errors.New("invalid auth scheme")
		}
		return "", errors.New("missing bearer token")
	}

	return "", errors.New("missing token")
}

func parseTokenPermissions(raw string) map[string]map[string]bool {
	parsed := map[string]map[string]bool{}
	for _, entry := range strings.Split(raw, ",") {
		item := strings.TrimSpace(entry)
		if item == "" {
			continue
		}
		parts := strings.SplitN(item, "=", 2)
		if len(parts) != 2 {
			continue
		}

		token := strings.TrimSpace(parts[0])
		permRaw := strings.TrimSpace(parts[1])
		if token == "" || permRaw == "" {
			continue
		}

		permSet := map[string]bool{}
		for _, p := range strings.Split(permRaw, "|") {
			permission := strings.TrimSpace(p)
			if permission != "" {
				permSet[permission] = true
			}
		}

		if len(permSet) > 0 {
			parsed[token] = permSet
		}
	}
	return parsed
}

func parsePublicPaths(raw string) map[string]bool {
	paths := map[string]bool{}
	for _, p := range strings.Split(raw, ",") {
		path := strings.TrimSpace(p)
		if path != "" {
			paths[path] = true
		}
	}
	if len(paths) == 0 {
		paths["/healthz"] = true
		paths["/ready"] = true
	}
	return paths
}

// isPublicPath reports whether path matches a configured public path,
// either exactly or as a sub-resource of one (e.g. "/patterns/123" matches
// the public path "/patterns"). This lets the outer Middleware step aside
// for route groups whose real auth decision is made by their own per-route
// middleware further down the chain (see CheckTokenPermission).
func isPublicPath(path string) bool {
	if publicPaths[path] {
		return true
	}
	for p := range publicPaths {
		if p != "" && strings.HasPrefix(path, p+"/") {
			return true
		}
	}
	return false
}

func secureEqual(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}