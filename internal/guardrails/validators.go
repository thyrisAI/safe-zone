package guardrails

import (
	"encoding/json"
	"encoding/xml"
	"errors"
	"regexp"
	"thyris-sz/internal/ai"
	"thyris-sz/internal/config"
	"thyris-sz/internal/repository"

	"github.com/xeipuuv/gojsonschema"
)

// isValidJSON checks if the string is valid JSON
func isValidJSON(s string) bool {
	var js interface{}
	return json.Unmarshal([]byte(s), &js) == nil
}

// isValidXML checks if the string is valid XML
func isValidXML(s string) bool {
	return xml.Unmarshal([]byte(s), new(interface{})) == nil
}

// isValidSchema validates JSON content against a given JSON Schema
// isValidSchema validates JSON content against a given JSON Schema
func isValidSchema(jsonContent string, schemaContent string) (bool, error) {
	schemaLoader := gojsonschema.NewStringLoader(schemaContent)
	documentLoader := gojsonschema.NewStringLoader(jsonContent)

	result, err := gojsonschema.Validate(schemaLoader, documentLoader)
	if err != nil {
		return false, err
	}

	if result.Valid() {
		return true, nil
	}

	// Collect validation errors
	var errMsg string
	for _, desc := range result.Errors() {
		errMsg += desc.String() + "; "
	}
	return false, errors.New(errMsg)
}

// ValidateFormat validates the text against a named format rule
func ValidateFormat(text string, formatName string) (bool, error) {
	validator, err := repository.GetValidatorByName(formatName)
	if err != nil {
		return false, errors.New("validator not found: " + formatName)
	}

	switch validator.Type {
	case "BUILTIN":
		switch validator.Name {
		case "JSON":
			return isValidJSON(text), nil
		case "XML":
			return isValidXML(text), nil
		default:
			return false, errors.New("unknown builtin validator: " + validator.Name)
		}
	case "REGEX":
		matched, err := regexp.MatchString(validator.Rule, text)
		if err != nil {
			return false, err
		}
		return matched, nil
	case "SCHEMA":
		if !config.AppConfig.Features.SchemaValidationEnabled {
			return true, nil // Skip validation if feature is disabled
		}
		// Ensure content is valid JSON first
		if !isValidJSON(text) {
			return false, errors.New("content is not valid JSON")
		}
		return isValidSchema(text, validator.Rule)
	case "AI_PROMPT":
		if !config.AppConfig.Features.SemanticAnalysisEnabled {
			// Security best practice: Fail Closed.
			return false, errors.New("AI validation is disabled by feature flag")
		}
		// Use AI Client to validate
		return ai.CheckWithAI(text, validator.Rule, validator.ExpectedResponse)
	default:
		return false, errors.New("unknown validator type: " + validator.Type)
	}
}
