package policy

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestPolicyDefinitionJSONRoundTrip(t *testing.T) {
	tenant := "tenant-a"
	want := PolicyDefinition{
		Scope: Scope{
			Tenant:      &tenant,
			Environment: "production",
			Gateway:     "public-gateway",
			Route:       "openai-chat",
		},
		Request: RequestPolicy{
			PII:              ActionMask,
			Secret:           ActionBlock,
			PromptInjection:  ActionAuditOnly,
			CustomPatternIDs: []string{"pattern-email", "pattern-account"},
			AllowlistIDs:     []string{"allow-support"},
			BlocklistIDs:     []string{"block-secrets"},
			CustomValidators: []ValidatorReference{
				{ID: "validator-json", Version: 3},
				{ID: "validator-injection", Version: 7},
			},
		},
		Response: ResponsePolicy{
			Enabled:          true,
			PII:              ActionMask,
			Secret:           ActionBlock,
			UnsafeContent:    ActionAuditOnly,
			CustomPatternIDs: []string{"response-pattern"},
			CustomValidators: []ValidatorReference{{ID: "response-validator", Version: 2}},
		},
		FailurePolicy: FailurePolicy{
			Request:  FailureModeClosed,
			Response: FailureModeOpen,
		},
		Limits: Limits{
			MaxBodyBytes:        1048576,
			ProcessingTimeoutMS: 2000,
			MaxDetections:       100,
		},
		Audit: AuditSettings{
			Enabled:           true,
			IncludeCategories: true,
		},
		Telemetry: TelemetrySettings{
			Enabled:        true,
			MetricsEnabled: true,
			TracingEnabled: true,
			SampleRate:     0.25,
		},
	}

	encoded, err := json.Marshal(want)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}

	var got PolicyDefinition
	if err := json.Unmarshal(encoded, &got); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("JSON round trip changed definition\ngot:  %#v\nwant: %#v", got, want)
	}
}
