package extproc

import "testing"

func TestProcessingStageValidate(t *testing.T) {
	tests := []struct {
		name    string
		stage   ProcessingStage
		wantErr bool
	}{
		{name: "request", stage: StageRequest},
		{name: "response", stage: StageResponse},
		{name: "empty", stage: "", wantErr: true},
		{name: "unknown", stage: "stream", wantErr: true},
		{name: "wrong case", stage: "REQUEST", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.stage.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("ProcessingStage(%q).Validate() error = %v, wantErr %v", test.stage, err, test.wantErr)
			}
		})
	}
}

func TestActionValidate(t *testing.T) {
	tests := []struct {
		name    string
		action  Action
		wantErr bool
	}{
		{name: "allow", action: ActionAllow},
		{name: "mask", action: ActionMask},
		{name: "block", action: ActionBlock},
		{name: "audit only", action: ActionAuditOnly},
		{name: "empty", action: "", wantErr: true},
		{name: "unknown", action: "REDACT", wantErr: true},
		{name: "wrong case", action: "allow", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := test.action.Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Action(%q).Validate() error = %v, wantErr %v", test.action, err, test.wantErr)
			}
		})
	}
}
