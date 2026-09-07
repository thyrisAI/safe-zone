package policy

import (
	"errors"
	"testing"
)

func TestValidateDefinitionAcceptsSupportedActions(t *testing.T) {
	actions := []Action{ActionAllow, ActionMask, ActionBlock, ActionAuditOnly}
	for _, action := range actions {
		t.Run(string(action), func(t *testing.T) {
			definition := validPolicyDefinition()
			definition.Request.PII = action
			definition.Request.Secret = action
			definition.Request.PromptInjection = action
			definition.Response.PII = action
			definition.Response.Secret = action
			definition.Response.UnsafeContent = action
			if err := ValidateDefinition(definition); err != nil {
				t.Fatalf("ValidateDefinition() error = %v", err)
			}
		})
	}
}

func TestValidateDefinitionRejectsUnsupportedActions(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PolicyDefinition)
	}{
		{name: "empty request PII", mutate: func(definition *PolicyDefinition) { definition.Request.PII = "" }},
		{name: "unknown request secret", mutate: func(definition *PolicyDefinition) { definition.Request.Secret = "REDACT" }},
		{name: "wrong-case prompt injection", mutate: func(definition *PolicyDefinition) { definition.Request.PromptInjection = "block" }},
		{name: "unknown response PII", mutate: func(definition *PolicyDefinition) { definition.Response.PII = "WARN" }},
		{name: "invalid disabled response action", mutate: func(definition *PolicyDefinition) {
			definition.Response.Enabled = false
			definition.Response.UnsafeContent = "PASS"
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := validPolicyDefinition()
			test.mutate(&definition)
			err := ValidateDefinition(definition)
			if !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("ValidateDefinition() error = %v, want ErrInvalidDefinition", err)
			}
		})
	}
}

func TestValidateDefinitionAllowsDisabledEmptyResponse(t *testing.T) {
	definition := validPolicyDefinition()
	definition.Response = ResponsePolicy{Enabled: false}
	if err := ValidateDefinition(definition); err != nil {
		t.Fatalf("ValidateDefinition() error = %v", err)
	}
}

func TestValidateDefinitionAcceptsBlockForWindowedStreamingHalt(t *testing.T) {
	definition := validPolicyDefinition()
	definition.Streaming = StreamingSettings{Mode: StreamingModeWindowed, WindowBytes: 4096}
	if err := ValidateDefinition(definition); err != nil {
		t.Fatalf("ValidateDefinition() error = %v", err)
	}
}

func TestValidateDefinitionAcceptsAsyncAuditOnlyStreaming(t *testing.T) {
	definition := validPolicyDefinition()
	definition.Response.PII = ActionAuditOnly
	definition.Response.Secret = ActionAuditOnly
	definition.Response.UnsafeContent = ActionAuditOnly
	definition.Streaming = StreamingSettings{Mode: StreamingModeAsyncAudit}
	if err := ValidateDefinition(definition); err != nil {
		t.Fatalf("ValidateDefinition() error = %v", err)
	}
}

func TestValidateDefinitionRejectsEnforcementActionsForAsyncAuditStreaming(t *testing.T) {
	for _, action := range []Action{ActionMask, ActionBlock} {
		t.Run(string(action), func(t *testing.T) {
			definition := validPolicyDefinition()
			definition.Response.PII = action
			definition.Response.Secret = ActionAuditOnly
			definition.Response.UnsafeContent = ActionAuditOnly
			definition.Streaming = StreamingSettings{Mode: StreamingModeAsyncAudit}
			if err := ValidateDefinition(definition); !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("ValidateDefinition() error = %v, want ErrInvalidDefinition", err)
			}
		})
	}
}

func TestValidateDefinitionRejectsStrictStreamingWithoutDowngradingIt(t *testing.T) {
	definition := validPolicyDefinition()
	definition.Streaming = StreamingSettings{Mode: "Strict"}
	err := ValidateDefinition(definition)
	if !errors.Is(err, ErrInvalidDefinition) {
		t.Fatalf("ValidateDefinition() error = %v, want ErrInvalidDefinition", err)
	}
	if err == nil || err.Error() != "invalid policy definition: strict streaming is unsupported; use buffered non-streaming response enforcement" {
		t.Fatalf("ValidateDefinition() error = %v, want strict-streaming guidance", err)
	}
}

func TestValidateDefinitionAcceptsMaskOnlyWindowedStreaming(t *testing.T) {
	definition := validPolicyDefinition()
	definition.Response.Secret = ActionMask
	definition.Streaming = StreamingSettings{Mode: StreamingModeWindowed, WindowBytes: 4096}
	if err := ValidateDefinition(definition); err != nil {
		t.Fatalf("ValidateDefinition() error = %v", err)
	}
}

func TestValidateDefinitionRejectsMalformedValidatorReferences(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(*PolicyDefinition)
	}{
		{name: "empty request validator ID", mutate: func(definition *PolicyDefinition) {
			definition.Request.CustomValidators = []ValidatorReference{{ID: "", Version: 1}}
		}},
		{name: "blank request validator ID", mutate: func(definition *PolicyDefinition) {
			definition.Request.CustomValidators = []ValidatorReference{{ID: "   ", Version: 1}}
		}},
		{name: "zero request validator version", mutate: func(definition *PolicyDefinition) {
			definition.Request.CustomValidators = []ValidatorReference{{ID: "validator", Version: 0}}
		}},
		{name: "negative request validator version", mutate: func(definition *PolicyDefinition) {
			definition.Request.CustomValidators = []ValidatorReference{{ID: "validator", Version: -1}}
		}},
		{name: "empty response validator ID", mutate: func(definition *PolicyDefinition) {
			definition.Response.CustomValidators = []ValidatorReference{{ID: "", Version: 2}}
		}},
		{name: "zero response validator version", mutate: func(definition *PolicyDefinition) {
			definition.Response.CustomValidators = []ValidatorReference{{ID: "validator", Version: 0}}
		}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := validPolicyDefinition()
			test.mutate(&definition)
			err := ValidateDefinition(definition)
			if !errors.Is(err, ErrInvalidDefinition) {
				t.Fatalf("ValidateDefinition() error = %v, want ErrInvalidDefinition", err)
			}
		})
	}
}

func validPolicyDefinition() PolicyDefinition {
	return PolicyDefinition{
		Request: RequestPolicy{
			PII:             ActionMask,
			Secret:          ActionBlock,
			PromptInjection: ActionAuditOnly,
		},
		Response: ResponsePolicy{
			Enabled:       true,
			PII:           ActionMask,
			Secret:        ActionBlock,
			UnsafeContent: ActionAuditOnly,
		},
		FailurePolicy: FailurePolicy{Request: FailureModeClosed, Response: FailureModeOpen},
	}
}
