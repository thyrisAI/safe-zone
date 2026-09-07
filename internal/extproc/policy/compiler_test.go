package policy

import (
	"bytes"
	"testing"
)

func TestCanonicalDefinitionIsDeterministic(t *testing.T) {
	definition := validPolicyDefinition()
	definition.Request.CustomPatternIDs = []string{"3", "1"}
	definition.Request.AllowlistIDs = []string{"2"}
	definition.Request.CustomValidators = []ValidatorReference{{ID: "4", Version: 7}}

	first, err := CanonicalDefinition(definition)
	if err != nil {
		t.Fatalf("CanonicalDefinition() error = %v", err)
	}
	second, err := CanonicalDefinition(definition)
	if err != nil {
		t.Fatalf("CanonicalDefinition() second error = %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatalf("canonical definition is not deterministic\nfirst:  %s\nsecond: %s", first, second)
	}
}

func TestSameDefinitionProducesSameIntegrityHash(t *testing.T) {
	definition := validPolicyDefinition()
	definition.Request.CustomPatternIDs = []string{"3", "1"}
	definition.Request.CustomValidators = []ValidatorReference{{ID: "4", Version: 7}}

	first, err := DefinitionIntegrityHash(definition)
	if err != nil {
		t.Fatalf("DefinitionIntegrityHash() error = %v", err)
	}
	second, err := DefinitionIntegrityHash(definition)
	if err != nil {
		t.Fatalf("DefinitionIntegrityHash() second error = %v", err)
	}
	if first != second {
		t.Fatalf("integrity hashes differ: first=%q second=%q", first, second)
	}
}
