package main

import "testing"

func TestPolicyResolutionMode(t *testing.T) {
	tests := []struct {
		name      string
		mode      string
		legacy    string
		want      string
		wantError bool
	}{
		{name: "defaults to header", want: "header"},
		{name: "selects header", mode: "header", want: "header"},
		{name: "selects attribute", mode: "attribute", want: "attribute"},
		{name: "normalizes whitespace and case", mode: " ATTRIBUTE ", want: "attribute"},
		{name: "uses legacy alias", legacy: "attribute", want: "attribute"},
		{name: "rejects unsupported mode", mode: "mixed", wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Setenv("TSZ_POLICY_RESOLUTION_MODE", test.mode)
			t.Setenv("TSZ_POLICY_RESOLVER", test.legacy)
			got, err := policyResolutionMode()
			if test.wantError {
				if err == nil {
					t.Fatal("policyResolutionMode() error = nil, want error")
				}
				return
			}
			if err != nil {
				t.Fatalf("policyResolutionMode() error = %v", err)
			}
			if got != test.want {
				t.Fatalf("policyResolutionMode() = %q, want %q", got, test.want)
			}
		})
	}
}
