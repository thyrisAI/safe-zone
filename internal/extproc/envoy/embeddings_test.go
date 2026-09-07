package envoy

import (
	"context"
	"strings"
	"testing"

	corev3 "github.com/envoyproxy/go-control-plane/envoy/config/core/v3"
	extprocv3 "github.com/envoyproxy/go-control-plane/envoy/service/ext_proc/v3"
	. "thyris-sz/internal/extproc"
	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

func TestEmbeddingsGRPCEnforcement(t *testing.T) {
	for _, tc := range []struct {
		name, input string
		mode        policy.FailureMode
		block, mask bool
	}{
		{"safe", `"ordinary text"`, policy.FailureModeClosed, false, false},
		{"mask", `["ordinary text","alice@example.com"]`, policy.FailureModeClosed, false, true},
		{"block entire batch", `["alice@example.com","secret-42"]`, policy.FailureModeClosed, true, false},
		{"tokens fail closed", `[1,2,3]`, policy.FailureModeClosed, true, false},
		{"tokens fail open", `[1,2,3]`, policy.FailureModeOpen, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			service, err := guardrails.NewGuardrailService(&guardrails.Detector{})
			if err != nil {
				t.Fatal(err)
			}
			processor, err := NewOpenAIRequestProcessor(service)
			if err != nil {
				t.Fatal(err)
			}
			cache := snapshotPolicyCache{snapshot: policy.CompiledSnapshot{PolicyID: "default", Version: 1, Definition: policy.PolicyDefinition{
				Request:       policy.RequestPolicy{PII: policy.ActionMask, Secret: policy.ActionBlock, PromptInjection: policy.ActionAuditOnly, CompiledRules: policy.CompiledRequestRules{CustomPatterns: []policy.CompiledPattern{{ID: "secret", Name: "SECRET", Category: "SECRET", Regex: `secret-[0-9]+`, Action: policy.ActionBlock}}}},
				Response:      policy.ResponsePolicy{Enabled: true, PII: policy.ActionMask, Secret: policy.ActionBlock, UnsafeContent: policy.ActionAuditOnly},
				FailurePolicy: policy.FailurePolicy{Request: tc.mode},
			}}}
			stream, err := newExternalProcessorTestClient(t, processor, cache).Process(context.Background())
			if err != nil {
				t.Fatal(err)
			}
			defer stream.CloseSend()
			headers := requestHeadersMessage("rid-embedding", "envoy-embedding", "default")
			headers.GetRequestHeaders().Headers.Headers = append(headers.GetRequestHeaders().Headers.Headers, &corev3.HeaderValue{Key: ":path", RawValue: []byte("/v1/embeddings")}, &corev3.HeaderValue{Key: "content-type", RawValue: []byte("application/json")})
			if err = stream.Send(headers); err != nil {
				t.Fatal(err)
			}
			if response, err := stream.Recv(); err != nil || response.GetImmediateResponse() != nil {
				t.Fatalf("headers=%v err=%v", response, err)
			}
			body := []byte(`{"model":"custom","input":` + tc.input + `}`)
			if err = stream.Send(requestBodyForAdapterTest(body, true)); err != nil {
				t.Fatal(err)
			}
			response, err := stream.Recv()
			if err != nil {
				t.Fatal(err)
			}
			if (response.GetImmediateResponse() != nil) != tc.block {
				t.Fatalf("block=%t response=%v", tc.block, response)
			}
			if tc.block {
				return
			}
			mutation := response.GetRequestBody().GetResponse().GetBodyMutation().GetBody()
			if tc.mask {
				if len(mutation) == 0 || strings.Contains(string(mutation), "alice@example.com") || !strings.Contains(string(mutation), "ordinary text") {
					t.Fatalf("mutation=%s", mutation)
				}
			} else if len(mutation) != 0 {
				t.Fatalf("unexpected mutation=%s", mutation)
			}
			for _, message := range []*extprocv3.ProcessingRequest{responseHeadersForAdapterTest(false), responseBodyForAdapterTest([]byte(`{"object":"list","data":[{"embedding":[0.1,-0.2]}],"usage":{"total_tokens":3}}`), true)} {
				if err = stream.Send(message); err != nil {
					t.Fatal(err)
				}
				response, err = stream.Recv()
				if err != nil || response.GetImmediateResponse() != nil {
					t.Fatalf("response=%v err=%v", response, err)
				}
				if message.GetResponseBody() != nil && response.GetResponseBody().GetResponse().GetBodyMutation() != nil {
					t.Fatalf("embedding output mutated: %v", response)
				}
			}
		})
	}
}
