package extproc

import (
	"mime"
	"strings"
)

const (
	genericJSONProvider       = "generic_json"
	GenericJSONContentAdapter = "generic-json"
)

// ParseGenericJSON treats one complete JSON value as structured content. The
// complete document is inspected so schema and semantic validators retain
// field relationships; masking must still produce the same top-level JSON
// kind before it can replace the original body.
func ParseGenericJSON(contentType string, body []byte, role string) (*ProviderPayload, error) {
	if !isGenericJSONContentType(contentType) {
		return nil, providerPayloadError(genericJSONProvider, "", ErrUnsupportedProviderContent)
	}
	parser := jsonSourceParser{source: body}
	root, err := parser.parseDocument()
	if err != nil {
		return nil, providerPayloadError(genericJSONProvider, "", ErrUnsupportedProviderContent)
	}
	if hasDuplicateJSONKeys(root) {
		return nil, providerPayloadError(genericJSONProvider, "", ErrUnsupportedProviderContent)
	}
	payload := newProviderPayload(genericJSONProvider, body)
	payload.addJSONDocument(role, "$", root)
	return payload, nil
}

func isGenericJSONContentType(contentType string) bool {
	mediaType, _, err := mime.ParseMediaType(contentType)
	if err != nil {
		return false
	}
	mediaType = strings.ToLower(mediaType)
	return mediaType == "application/json" ||
		(strings.HasPrefix(mediaType, "application/") && strings.HasSuffix(mediaType, "+json"))
}

func isGenericJSONSelected(request ProcessingRequest) bool {
	return strings.EqualFold(strings.TrimSpace(request.ContentAdapter), GenericJSONContentAdapter)
}
