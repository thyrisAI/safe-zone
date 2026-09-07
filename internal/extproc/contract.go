// This file defines the gateway-neutral BYG processing contract. Gateway
// adapters translate their transport-specific messages to and from these
// types; the contract itself does not depend on Envoy, gRPC, or Kubernetes.
package extproc

import (
	"fmt"
	"strings"

	"thyris-sz/internal/extproc/policy"
)

type ProcessingStage string

// CloneHeaders returns a deep copy suitable for safely crossing a transport
// boundary without sharing adapter-owned state.
func CloneHeaders(headers map[string][]string) map[string][]string {
	clone := make(map[string][]string, len(headers))
	for key, values := range headers {
		clone[key] = append([]string(nil), values...)
	}
	return clone
}

// FirstHeader returns the first value for a case-normalized header map.
func FirstHeader(headers map[string][]string, key string) string {
	values := headers[strings.ToLower(key)]
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

const (
	StageRequest  ProcessingStage = "request"
	StageResponse ProcessingStage = "response"
)

func (stage ProcessingStage) Validate() error {
	switch stage {
	case StageRequest, StageResponse:
		return nil
	default:
		return fmt.Errorf("invalid processing stage %q", stage)
	}
}

type ProcessingRequest struct {
	// RequestPath is the original request URI, retained for response processing.
	// Adapters must derive it from request routing data, never response headers.
	RequestPath string
	// RPCMethod correlates a request body with a response whose wire format does
	// not repeat the method, such as MCP JSON-RPC over Streamable HTTP.
	RPCMethod  string
	RID        string
	EnvoyReqID string
	// TraceID remains empty unless a future trusted gateway source supplies it.
	// Client-controlled trace headers are intentionally not trusted here.
	TraceID string
	// TraceParent is accepted only from gateway-configured trusted attributes.
	// It is used to continue W3C trace context and is never exposed as metadata.
	TraceParent    string
	Stage          ProcessingStage
	Headers        map[string][]string
	Body           []byte
	ContentType    string
	Gateway        string
	Route          string
	Tenant         string
	Attributes     map[string]string
	PolicyID       string
	PolicyVersion  int
	FailureMode    policy.FailureMode
	PolicySnapshot *policy.CompiledSnapshot
	EndOfStream    bool
}

type Action string

const (
	ActionAllow     Action = "ALLOW"
	ActionMask      Action = "MASK"
	ActionBlock     Action = "BLOCK"
	ActionAuditOnly Action = "AUDIT_ONLY"
)

func (action Action) Validate() error {
	switch action {
	case ActionAllow, ActionMask, ActionBlock, ActionAuditOnly:
		return nil
	default:
		return fmt.Errorf("invalid processing action %q", action)
	}
}

type ProcessingResult struct {
	Action          Action
	Body            []byte
	HeaderMutations map[string]string
	PolicyVersion   int
	DetectionCount  int
	Degraded        bool
	ImmediateStatus int
	Metadata        SafeMetadata
}

// SafeMetadata is safe to expose as gateway dynamic metadata. It contains
// identifiers and aggregate detection information only, never raw content or
// detected values.
type SafeMetadata struct {
	RequestID          string          `json:"request_id"`
	RID                string          `json:"rid"`
	PolicyID           string          `json:"policy_id"`
	PolicyVersion      int             `json:"policy_version"`
	Adapter            string          `json:"adapter"`
	Stage              ProcessingStage `json:"stage"`
	Action             Action          `json:"action"`
	Categories         []string        `json:"categories"`
	DetectionCount     int             `json:"detection_count"`
	ProcessorLatencyMS int64           `json:"processor_latency_ms"`
	Degraded           bool            `json:"degraded"`
}
