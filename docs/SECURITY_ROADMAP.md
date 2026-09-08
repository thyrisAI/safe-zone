# TSZ Security Hardening Roadmap

This document outlines the security enhancements required to make TSZ production‑ready for security‑sensitive environments (banking, fintech, healthcare, PII-critical workloads).

---

## Overview

TSZ currently provides **content detection and guardrails** for PII/sensitive data, but lacks **operational security controls** such as authentication, authorization, rate limiting, and transport security. This roadmap addresses those gaps systematically.

**Timeline:** 17 August-25 October 2026 (10 weeks, Q3-Q4 2026)

**Schedule baseline:** Rebaselined in mid-August 2026. Week 1 starts on 17 August 2026.

**Priority:** CRITICAL – Must be completed before Phase 4 (public production releases)

---

## Milestone 1: HTTP Security Hardening (17-30 August 2026; Weeks 1-2)

### Goal
Make the HTTP layer production-ready with standard security controls.

### Tasks

#### 1.1 Add Request-Level Security
- [x] **Request size limits**: Implement `http.MaxBytesReader` on all POST endpoints
  - Default: 10 MB per request
  - Configurable via `MAX_REQUEST_SIZE_BYTES` env var
  - Error: Return `413 Payload Too Large` if exceeded
  
- [x] **Request timeouts**: Enforce per-handler timeouts
  - `/detect`: 30s timeout (default)
  - `/v1/chat/completions`: 5m timeout (LLM calls may be slow)
  - Configurable via `HANDLER_TIMEOUT_SECONDS` env vars

- [x] **Connection limits**: Configure `http.Server` with:
  ```go
  server := &http.Server{
    ReadTimeout:  15 * time.Second,
    WriteTimeout: 15 * time.Second,
    IdleTimeout:  60 * time.Second,
    MaxHeaderBytes: 1 << 20, // 1MB
  }
  ```

#### 1.2 Add Security Headers Middleware
- [x] Create `internal/middleware/security_headers.go`
- [x] Apply to all responses:
  - `X-Frame-Options: DENY` (prevent clickjacking)
  - `X-Content-Type-Options: nosniff` (disable MIME sniffing)
  - `X-XSS-Protection: 1; mode=block` (XSS protection)
  - `Cache-Control: no-cache, no-store, must-revalidate` (prevent caching sensitive data)
  - `Pragma: no-cache`
  - `Expires: 0`
  - `Strict-Transport-Security: max-age=63072000; includeSubDomains` (TLS only, requires HTTPS)

#### 1.3 Add CORS Middleware
- [x] Create `internal/middleware/cors.go`
- [x] Configuration via environment:
  ```
  CORS_ALLOWED_ORIGINS=https://example.com,https://api.example.com
  CORS_ALLOWED_METHODS=GET,POST,OPTIONS
  CORS_ALLOWED_HEADERS=Content-Type,Authorization
  CORS_MAX_AGE=86400
  ```
- [x] Default: Deny all (fail-secure)
- [x] Return `403 Forbidden` for disallowed origins

#### 1.4 Add Input Validation Middleware
- [x] Validate Content-Type on POST/PUT endpoints (must be `application/json`)
- [x] Add JSON body validation for request bodies
- [x] Return `400 Bad Request` with validation errors

---

## Milestone 2: Authentication & Authorization (24 August-13 September 2026; Weeks 2-4)

### Goal
Protect all endpoints with API key / Bearer token authentication and role-based access control.

### Tasks

#### 2.1 Authentication Layer
- [x] Create `internal/auth/auth.go` with:
  - Bearer token validation
  - API key validation (stored hashed in database)
  - Custom claims (user ID, permissions, org ID for multi-tenant support)

- [ ] Database schema for API keys:
  ```sql
  CREATE TABLE api_keys (
    id UUID PRIMARY KEY,
    user_id UUID NOT NULL,
    name VARCHAR(255),
    token_hash BYTEA NOT NULL UNIQUE,
    revoked BOOLEAN DEFAULT false,
    last_used_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT now(),
    expires_at TIMESTAMP,
    permissions JSONB DEFAULT '[]',
    FOREIGN KEY (user_id) REFERENCES users(id)
  );
  ```

- [x] Authentication middleware:
  ```go
  func AuthMiddleware(next http.Handler) http.Handler {
    return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
      token := extractToken(r)
      user, err := ValidateToken(token)
      // Store user in request context
      ctx := context.WithValue(r.Context(), "user", user)
      next.ServeHTTP(w, r.WithContext(ctx))
    })
  }
  ```

#### 2.2 Authorization Rules
- [x] Define permission model:
  ```
  - detect:read
  - gateway:use
  - patterns:admin
  - validators:admin
  - allowlist:admin
  - blacklist:admin
  - templates:admin
  - cache:admin
  ```

- [x] Implement role-based access control (RBAC):
  ```go
  // Check permission in handlers
  if !hasPermission(user, "patterns:admin") {
    return http.StatusForbidden
  }
  ```

#### 2.3 Public vs Protected Endpoints
- [x] **Public (health checks only)**:
  - `GET /healthz` (no auth)
  - `GET /ready` (no auth)

- [x] **Protected (require API key)**:
  - All other endpoints require valid Bearer token

- [x] **Admin (require admin role)**:
  - `POST /admin/reload`
  - `POST /patterns`
  - `POST /validators`
  - etc.

#### 2.4 API Key Management Endpoints
- [ ] `POST /api-keys`: Create new API key
- [ ] `GET /api-keys`: List user's API keys (non-sensitive)
- [ ] `DELETE /api-keys/{id}`: Revoke API key
- [ ] `POST /api-keys/{id}/rotate`: Rotate existing key

---

## Milestone 3: Rate Limiting & DDoS Protection (7-20 September 2026; Weeks 4-5)

### Goal
Prevent abuse with rate limiting and traffic shaping.

### Tasks

#### 3.1 Global Rate Limiting
- [x] Implement token-bucket rate limiter in `internal/middleware/ratelimit.go`
- [x] Configuration:
  ```
  RATELIMIT_ENABLED=true
  RATELIMIT_REQUESTS_PER_SECOND=100
  RATELIMIT_BURST=1000
  ```

#### 3.2 Per-User Rate Limiting
- [x] Apply stricter limits per API key:
  ```
  /detect:       1000 req/min per key
  /chat/completions: 100 req/min per key
  /patterns:     50 req/min per key
  /admin/*:      10 req/min per key
  ```

- [ ] Store in Redis for distributed rate limiting:
  ```go
  key := fmt.Sprintf("rl:%s:%s", userID, endpoint)
  count := cache.IncrementWithExpiry(key, 60*time.Second)
  if count > limit {
    return http.StatusTooManyRequests
  }
  ```

#### 3.3 Endpoint-Specific DDoS Protection
- [ ] `/detect`: Identify and block clients hitting with massive payloads
- [ ] `/v1/chat/completions`: Limit concurrent streams per key
- [ ] `/admin`: Limit to specific IP ranges (whitelist)

---

## Milestone 4: Data Protection & Encryption (14-27 September 2026; Weeks 5-6)

### Goal
Encrypt sensitive data at rest and in transit.

### Tasks

#### 4.1 Transport Security (TLS)
- [ ] Make HTTPS mandatory:
  ```go
  server := &http.Server{
    Addr: ":8443",
    TLSConfig: &tls.Config{
      MinVersion: tls.VersionTLS13,
      CipherSuites: []uint16{
        tls.TLS_AES_256_GCM_SHA384,
        tls.TLS_CHACHA20_POLY1305_SHA256,
      },
    },
  }
  server.ListenAndServeTLS(certFile, keyFile)
  ```

- [ ] Configuration:
  ```
  TLS_ENABLED=true
  TLS_CERT_FILE=/etc/tls/cert.pem
  TLS_KEY_FILE=/etc/tls/key.pem
  TLS_MIN_VERSION=1.3
  ```

- [ ] HTTP -> HTTPS redirect (optional on port 80):
  ```go
  http.ListenAndServe(":80", http.HandlerFunc(redirectHTTPS))
  ```

#### 4.2 Database Encryption
- [ ] PostgreSQL connections:
  ```
  DB_DSN=postgres://user:pass@host:5432/db?sslmode=require&sslkey=...&sslcert=...
  ```

- [ ] Encrypt sensitive fields at-rest:
  ```go
  type APIKey struct {
    ID          uuid.UUID
    TokenHash   []byte // hashed + salted
    Permissions pq.JSONBArray
    EncryptedMetadata string // encrypted_pk(...)
  }
  ```

#### 4.3 Redis Encryption
- [ ] TLS for Redis connections:
  ```
  REDIS_URL=rediss://:password@host:6379/0?ssl_cert_file=...&ssl_key_file=...
  ```

- [ ] Alternative: Encrypt sensitive values before writing to Redis

#### 4.4 Secret Management
- [ ] Do NOT log or expose:
  - API keys
  - Database passwords
  - AWS credentials
  - Encryption keys
  
- [ ] Use secrets manager (HashiCorp Vault, AWS Secrets Manager):
  ```go
  // Initialize on startup
  cfg := config.LoadConfig() // reads from Vault, not env
  log.Printf("Config loaded (secrets redacted)")
  ```

---

## Milestone 5: Audit Logging & Monitoring (21 September-4 October 2026; Weeks 6-7)

### Goal
Enable security events logging and alerting.

### Tasks

#### 5.1 Audit Log Schema
- [ ] Create `audit_logs` table:
  ```sql
  CREATE TABLE audit_logs (
    id UUID PRIMARY KEY,
    timestamp TIMESTAMP DEFAULT now(),
    user_id UUID,
    action VARCHAR(100),
    resource_type VARCHAR(50),
    resource_id VARCHAR(100),
    status VARCHAR(20),
    result_summary TEXT,
    ip_address INET,
    user_agent TEXT,
    details JSONB,
    created_at TIMESTAMP DEFAULT now()
  );
  CREATE INDEX ON audit_logs(user_id, timestamp DESC);
  CREATE INDEX ON audit_logs(timestamp DESC);
  ```

#### 5.2 Log Security Events
- [ ] Authentication events:
  - Yes Successful login (with key ID, IP, timestamp)
  - No Failed login attempts (with IP)
  - Warning Suspicious patterns (multiple failures from same IP)

- [ ] Authorization events:
  - Yes Permission granted
  - No Permission denied
  - No Attempted admin action without privileges

- [ ] Data access events:
  - Patterns CRUD operations
  - Allowlist/blocklist modifications
  - Template imports

- [ ] System events:
  - Cache reloads
  - Configuration changes
  - Warnings/errors in detection/guardrails

#### 5.3 Structured Logging
- [ ] Use JSON structured logging:
  ```go
  log.WithFields(logrus.Fields{
    "timestamp": time.Now(),
    "user_id":   userID,
    "action":    "api_key_created",
    "ip":        clientIP,
    "status":    "success",
  }).Info("API key created")
  ```

#### 5.4 SIEM Integration
- [ ] Enhance existing SIEM handler in `internal/guardrails/siem.go`
- [ ] Forward audit logs to external SIEM (Splunk, ELK, etc.)
- [ ] Field mapping:
  ```json
  {
    "timestamp": "2026-04-23T10:00:00Z",
    "event_type": "auth.login_success",
    "user_id": "user-123",
    "resource": "api_key#456",
    "action": "create",
    "result": "success",
    "severity": "INFO|WARN|ERROR|CRITICAL"
  }
  ```

---

## Milestone 6: Vulnerability Management (28 September-11 October 2026; Weeks 7-8)

### Goal
Build automated scanning and dependency management.

### Tasks

#### 6.1 Dependency Scanning
- [ ] Add GitHub Actions workflow (`.github/workflows/security.yml`):
  ```yaml
  - name: Run go vulnerability check
    run: go run golang.org/x/vuln/cmd/govulncheck@latest ./...
  
  - name: OWASP dependency check
    run: |
      curl https://github.com/jeremylong/DependencyCheck/releases/download/v8.0.0/dependency-check_linux_x64.sh
      bash dependency-check.sh --scan . --format ALL
  ```

- [ ] Enable GitHub Dependabot for Go modules (auto-updates)

#### 6.2 SAST (Static Analysis)
- [ ] Add `gosec` (Go security analyzer):
  ```yaml
  - name: Run gosec
    run: go run github.com/securego/gosec/v2/cmd/gosec@latest -no-fail -fmt json ./... | tee gosec-report.json
  ```
  
- [ ] Configure exclusions in `.gosec` file

#### 6.3 Container Scanning
- [ ] Scan Docker images with Trivy:
  ```yaml
  - name: Run Trivy vulnerability scanner
    run: trivy image --severity HIGH,CRITICAL thyris-sz:latest
  ```

#### 6.4 Security Policy & SLA
- [ ] Define SLA for vulnerability fixes:
  - **CRITICAL**: Fix within 24 hours
  - **HIGH**: Fix within 7 days
  - **MEDIUM**: Fix within 30 days
  - **LOW**: Fix in next release cycle

---

## Milestone 7: Production Hardening & Deployment (5-18 October 2026; Weeks 8-9)

### Goal
Make deployment and operations secure.

### Tasks

#### 7.1 Configuration Hardening
- [ ] Remove default credentials from `deployment/docker/docker-compose.yml` and `scripts/database/init.sql`
- [ ] Generate strong random passwords during deployment
- [ ] Support .env file variable rotation

#### 7.2 Network Security
- [ ] Document recommended network topology:
  ```
  [ Reverse Proxy (TLS) ] -> [ TSZ ] -> [ LB ] -> [ PostgreSQL (private) ]
                            v
                         [ Redis (private) ]
  ```

- [ ] Network policy examples (Kubernetes/Docker):
  - Restrict database access to TSZ pods only
  - Restrict Redis access to TSZ pods only
  - Only allow HTTPS traffic to TSZ

#### 7.3 Runtime Security
- [ ] Run as non-root user in Docker:
  ```dockerfile
  USER tsz:tsz
  ```

- [ ] Use read-only root filesystem where possible
- [ ] Set resource limits (memory, CPU):
  ```yaml
  resources:
    limits:
      memory: "512Mi"
      cpu: "500m"
```

#### 7.4 Security Testing Automation
- [ ] Add security test suite:
  ```bash
  go test -tags=security ./tests/security/...
  ```
  
- [ ] Tests cover:
  - Authentication bypass attempts
  - Authorization bypass attempts
  - SQL injection resistance
  - XSS injection resistance
  - Rate limiting enforcement
  - TLS configuration validation

#### 7.5 Secrets Rotation
- [ ] Implement automated secret rotation:
  - API keys: 90-day rotation
  - Database passwords: 90-day rotation
  - TLS certificates: 30-day before expiry alert

---

## Milestone 8: Documentation & Guidelines (12-25 October 2026; Weeks 9-10)

### Goal
Document all security controls and best practices.

### Tasks

#### 8.1 Security Documentation
- [ ] Create `docs/SECURITY_OPERATIONS.md`:
  - How to deploy TSZ securely
  - How to manage API keys
  - How to set up TLS certificates
  - How to configure secrets manager
  - Monitoring and alerting setup

- [x] Update `docs/ARCHITECTURE_SECURITY.md`:
  - Add section on authentication/authorization
  - Add section on rate limiting
  - Add section on audit logging

#### 8.2 API Documentation
- [x] Add authentication examples to `docs/API_REFERENCE.md`:
  - How to obtain API key
  - How to include Bearer token
  - Error codes for auth failures

#### 8.3 Operational Runbooks
- [ ] Create `docs/RUNBOOKS.md`:
  - Incident response for compromised API key
  - Incident response for suspicious activity
  - Secret rotation procedure
  - Certificate renewal procedure
  - Upgrade procedure (with security patches)

#### 8.4 Contributing Security Guidelines
- [x] Update `CONTRIBUTING.md` with:
  - Security checklist for PRs
  - Dependency update policy
  - Code review security focus areas
  - How to report security issues in PRs

---

## Summary Timeline

| Phase | Milestone | Effort | Target |
|-------|-----------|--------|--------|
| 1 | HTTP Security Hardening | 1-2 weeks | 17-30 Aug 2026 (Weeks 1-2) |
| 2 | Authentication & Authorization | 2-3 weeks | 24 Aug-13 Sep 2026 (Weeks 2-4) |
| 3 | Rate Limiting & DDoS | 1 week | 7-20 Sep 2026 (Weeks 4-5) |
| 4 | Data Protection & TLS | 1 week | 14-27 Sep 2026 (Weeks 5-6) |
| 5 | Audit Logging & Monitoring | 1-2 weeks | 21 Sep-4 Oct 2026 (Weeks 6-7) |
| 6 | Dependency Scanning | 1 week | 28 Sep-11 Oct 2026 (Weeks 7-8) |
| 7 | Production Hardening | 1 week | 5-18 Oct 2026 (Weeks 8-9) |
| 8 | Documentation & Guidelines | 1 week | 12-25 Oct 2026 (Weeks 9-10) |
| **Total** | | **10 weeks** | **17 Aug-25 Oct 2026 (Q3-Q4)** |

---

## Success Criteria

TSZ will be considered **production-ready** when:

- [x] All endpoints (except `/healthz`, `/ready`) require authentication (when `AUTH_ENABLED=true`)
- [ ] All endpoints have rate limiting per API key
- [x] All responses include security headers
- [ ] Request/response bodies are validated
- [ ] TLS is mandatory (HTTPS only)
- [ ] Audit logging captures security events
- [ ] Database and Redis use encrypted connections
- [ ] Vulnerability scanning runs in CI/CD
- [ ] All security controls are documented
- [ ] Security tests pass (100% of auth/authz scenarios)
- [ ] No hardcoded credentials in code/config
- [ ] Production config examples provided

---

## Integration with Main Roadmap

This roadmap is part of **Phase 1 – Core Product Hardening** (see `ROADMAP.md`) and must be completed before:

- Phase 4: Public Production Release
- Phase 5: Multi-tenancy & Enterprise Features
- Phase 6: Compliance & Certifications (SOC2, FedRAMP, etc.)

---

## References

- `SECURITY.md` – Vulnerability disclosure policy
- `ARCHITECTURE_SECURITY.md` – System architecture & current controls
- `docs/API_REFERENCE.md` – API endpoint documentation
- `CONTRIBUTING.md` – Development guidelines
- OWASP Top 10 Web Application Security Risks: https://owasp.org/www-project-top-ten/
- Go Security Best Practices: https://golang.org/doc/effective_go#security
