package extproc

import "fmt"

const a2aProvider = "a2a_jsonrpc"

var a2aMethods = map[string]struct{}{
	"message/send":                        {},
	"message/stream":                      {},
	"tasks/send":                          {},
	"tasks/sendSubscribe":                 {},
	"tasks/get":                           {},
	"tasks/cancel":                        {},
	"tasks/resubscribe":                   {},
	"tasks/pushNotificationConfig/set":    {},
	"tasks/pushNotificationConfig/get":    {},
	"tasks/pushNotificationConfig/list":   {},
	"tasks/pushNotificationConfig/delete": {},
	"agent/getAuthenticatedExtendedCard":  {},
}

func ParseA2ARequest(contentType string, body []byte) (*ProviderPayload, error) {
	root, err := parseA2ADocument(contentType, body)
	if err != nil {
		return nil, err
	}
	method := root.object["method"]
	if method == nil || method.kind != jsonString || !isA2AMethod(method.stringValue) {
		return nil, providerPayloadError(a2aProvider, ".method", ErrUnsupportedProviderPayload)
	}
	payload := newProviderPayload(a2aProvider, body)
	switch method.stringValue {
	case "message/stream", "tasks/sendSubscribe", "tasks/resubscribe":
		return nil, providerPayloadError(a2aProvider, ".method", ErrUnsupportedProviderContent)
	case "message/send", "tasks/send":
		params := root.object["params"]
		if params == nil || params.kind != jsonObject || params.duplicateKeys {
			return nil, providerPayloadError(a2aProvider, ".params", ErrUnsupportedProviderContent)
		}
		message := params.object["message"]
		if err := appendA2AMessage(payload, "user", ".params.message", message); err != nil {
			return nil, err
		}
	}
	return payload, nil
}

func ParseA2AResponse(contentType string, body []byte, method string) (*ProviderPayload, error) {
	root, err := parseA2ADocument(contentType, body)
	if err != nil {
		return nil, err
	}
	payload := newProviderPayload(a2aProvider, body)
	if root.object["error"] != nil {
		return payload, nil
	}
	if method != "message/send" && method != "tasks/send" {
		return payload, nil
	}
	result := root.object["result"]
	if result == nil || result.kind != jsonObject || result.duplicateKeys {
		return nil, providerPayloadError(a2aProvider, ".result", ErrUnsupportedProviderContent)
	}

	// Some agentgateway-compatible A2A servers return a Message directly,
	// while task-oriented servers return message/status/history/artifacts.
	if result.object["role"] != nil || result.object["parts"] != nil {
		if err := appendA2AMessage(payload, "agent", ".result", result); err != nil {
			return nil, err
		}
	}
	if message := result.object["message"]; message != nil {
		if err := appendA2AMessage(payload, "agent", ".result.message", message); err != nil {
			return nil, err
		}
	}
	if status := result.object["status"]; status != nil {
		if status.kind != jsonObject || status.duplicateKeys {
			return nil, providerPayloadError(a2aProvider, ".result.status", ErrUnsupportedProviderContent)
		}
		if message := status.object["message"]; message != nil {
			if err := appendA2AMessage(payload, "agent", ".result.status.message", message); err != nil {
				return nil, err
			}
		}
	}
	if history := result.object["history"]; history != nil {
		if history.kind != jsonArray {
			return nil, providerPayloadError(a2aProvider, ".result.history", ErrUnsupportedProviderContent)
		}
		for index, message := range history.array {
			if err := appendA2AMessage(payload, "", fmt.Sprintf(".result.history[%d]", index), message); err != nil {
				return nil, err
			}
		}
	}
	if artifacts := result.object["artifacts"]; artifacts != nil {
		if artifacts.kind != jsonArray {
			return nil, providerPayloadError(a2aProvider, ".result.artifacts", ErrUnsupportedProviderContent)
		}
		for index, artifact := range artifacts.array {
			path := fmt.Sprintf(".result.artifacts[%d]", index)
			if artifact.kind != jsonObject || artifact.duplicateKeys {
				return nil, providerPayloadError(a2aProvider, path, ErrUnsupportedProviderContent)
			}
			if err := appendA2AParts(payload, "agent", path+".parts", artifact.object["parts"]); err != nil {
				return nil, err
			}
		}
	}
	return payload, nil
}

func parseA2ADocument(contentType string, body []byte) (*jsonNode, error) {
	root, err := parseProviderDocument(a2aProvider, contentType, body)
	if err != nil {
		return nil, err
	}
	version := root.object["jsonrpc"]
	if version == nil || version.kind != jsonString || version.stringValue != "2.0" {
		return nil, providerPayloadError(a2aProvider, ".jsonrpc", ErrUnsupportedProviderPayload)
	}
	if root.duplicateKeys {
		return nil, providerPayloadError(a2aProvider, "", ErrUnsupportedProviderContent)
	}
	return root, nil
}

func appendA2AMessage(payload *ProviderPayload, expectedRole, path string, message *jsonNode) error {
	if message == nil || message.kind != jsonObject || message.duplicateKeys {
		return providerPayloadError(a2aProvider, path, ErrUnsupportedProviderContent)
	}
	role := message.object["role"]
	if role == nil || role.kind != jsonString || !validA2ARole(role.stringValue) {
		return providerPayloadError(a2aProvider, path+".role", ErrUnsupportedProviderContent)
	}
	if expectedRole == "user" && role.stringValue != "user" && role.stringValue != "ROLE_USER" {
		return providerPayloadError(a2aProvider, path+".role", ErrUnsupportedProviderContent)
	}
	if expectedRole == "agent" && role.stringValue != "agent" && role.stringValue != "assistant" && role.stringValue != "ROLE_AGENT" {
		return providerPayloadError(a2aProvider, path+".role", ErrUnsupportedProviderContent)
	}
	return appendA2AParts(payload, role.stringValue, path+".parts", message.object["parts"])
}

func appendA2AParts(payload *ProviderPayload, role, path string, parts *jsonNode) error {
	if parts == nil || parts.kind != jsonArray || len(parts.array) == 0 {
		return providerPayloadError(a2aProvider, path, ErrUnsupportedProviderContent)
	}
	for index, part := range parts.array {
		if err := appendA2APart(payload, role, fmt.Sprintf("%s[%d]", path, index), part); err != nil {
			return err
		}
	}
	return nil
}

func appendA2APart(payload *ProviderPayload, role, path string, part *jsonNode) error {
	if part == nil || part.kind != jsonObject || part.duplicateKeys {
		return providerPayloadError(a2aProvider, path, ErrUnsupportedProviderContent)
	}
	typeNode, kindNode := part.object["type"], part.object["kind"]
	if typeNode != nil && kindNode != nil {
		return providerPayloadError(a2aProvider, path, ErrUnsupportedProviderContent)
	}
	discriminator := ""
	if typeNode != nil {
		if typeNode.kind != jsonString {
			return providerPayloadError(a2aProvider, path+".type", ErrUnsupportedProviderContent)
		}
		discriminator = typeNode.stringValue
	} else if kindNode != nil {
		if kindNode.kind != jsonString {
			return providerPayloadError(a2aProvider, path+".kind", ErrUnsupportedProviderContent)
		}
		discriminator = kindNode.stringValue
	}

	text, data := part.object["text"], part.object["data"]
	file, raw, url := part.object["file"], part.object["raw"], part.object["url"]
	contentFields := 0
	for _, node := range []*jsonNode{text, data, file, raw, url} {
		if node != nil {
			contentFields++
		}
	}
	if contentFields != 1 {
		return providerPayloadError(a2aProvider, path, ErrUnsupportedProviderContent)
	}

	switch {
	case text != nil && (discriminator == "" || discriminator == "text"):
		if text.kind != jsonString {
			return providerPayloadError(a2aProvider, path+".text", ErrUnsupportedProviderContent)
		}
		payload.addString(role, path+".text", text)
	case data != nil && (discriminator == "" || discriminator == "data"):
		if data.kind != jsonObject || hasDuplicateJSONKeys(data) {
			return providerPayloadError(a2aProvider, path+".data", ErrUnsupportedProviderContent)
		}
		payload.addJSONObject(role, path+".data", data)
	case file != nil && discriminator == "file":
		if file.kind != jsonObject || hasDuplicateJSONKeys(file) {
			return providerPayloadError(a2aProvider, path+".file", ErrUnsupportedProviderContent)
		}
		bytesNode, uriNode := file.object["bytes"], file.object["uri"]
		if (bytesNode == nil) == (uriNode == nil) {
			return providerPayloadError(a2aProvider, path+".file", ErrUnsupportedProviderContent)
		}
		if bytesNode != nil && bytesNode.kind != jsonString {
			return providerPayloadError(a2aProvider, path+".file.bytes", ErrUnsupportedProviderContent)
		}
		if uriNode != nil && uriNode.kind != jsonString {
			return providerPayloadError(a2aProvider, path+".file.uri", ErrUnsupportedProviderContent)
		}
	case raw != nil && discriminator == "":
		if raw.kind != jsonString {
			return providerPayloadError(a2aProvider, path+".raw", ErrUnsupportedProviderContent)
		}
	case url != nil && discriminator == "":
		if url.kind != jsonString {
			return providerPayloadError(a2aProvider, path+".url", ErrUnsupportedProviderContent)
		}
	default:
		return providerPayloadError(a2aProvider, path, ErrUnsupportedProviderContent)
	}
	return nil
}

func isA2AMessage(contentType string, body []byte) bool {
	// Detect by protocol identity and method before full validation so malformed
	// covered A2A messages fail through this adapter instead of falling through
	// to the generic MCP JSON-RPC path.
	root, err := parseProviderDocument(a2aProvider, contentType, body)
	if err != nil {
		return false
	}
	version := root.object["jsonrpc"]
	if version == nil || version.kind != jsonString || version.stringValue != "2.0" {
		return false
	}
	method := root.object["method"]
	return method != nil && method.kind == jsonString && isA2AMethod(method.stringValue)
}

func isA2AMethod(method string) bool {
	_, found := a2aMethods[method]
	return found
}

func validA2ARole(role string) bool {
	switch role {
	case "user", "agent", "assistant", "ROLE_USER", "ROLE_AGENT":
		return true
	default:
		return false
	}
}
