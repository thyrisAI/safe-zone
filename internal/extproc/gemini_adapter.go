package extproc

import "fmt"

const geminiProvider = "gemini_generate_content"

func ParseGeminiRequest(contentType string, body []byte) (*ProviderPayload, error) {
	root, err := parseProviderDocument(geminiProvider, contentType, body)
	if err != nil {
		return nil, err
	}
	contents := root.object["contents"]
	if contents == nil {
		return nil, providerPayloadError(geminiProvider, ".contents", ErrUnsupportedProviderPayload)
	}
	if contents.kind != jsonArray {
		return nil, providerPayloadError(geminiProvider, ".contents", ErrUnsupportedProviderContent)
	}
	payload := newProviderPayload(geminiProvider, body)
	if system := root.object["systemInstruction"]; system != nil {
		if err := appendGeminiContent(payload, "system", ".systemInstruction", system); err != nil {
			return nil, err
		}
	}
	if len(contents.array) == 0 {
		return nil, providerPayloadError(geminiProvider, ".contents", ErrUnsupportedProviderContent)
	}
	for index, content := range contents.array {
		path := fmt.Sprintf(".contents[%d]", index)
		role := "user"
		if roleNode := content.object["role"]; roleNode != nil {
			if roleNode.kind != jsonString || (roleNode.stringValue != "user" && roleNode.stringValue != "model" && roleNode.stringValue != "function") {
				return nil, providerPayloadError(geminiProvider, path+".role", ErrUnsupportedProviderContent)
			}
			role = roleNode.stringValue
		}
		if err := appendGeminiContent(payload, role, path, content); err != nil {
			return nil, err
		}
	}
	return payload, nil
}

func ParseGeminiResponse(contentType string, body []byte) (*ProviderPayload, error) {
	root, err := parseProviderDocument(geminiProvider, contentType, body)
	if err != nil {
		return nil, err
	}
	candidates := root.object["candidates"]
	if candidates == nil {
		if root.object["promptFeedback"] != nil {
			return newProviderPayload(geminiProvider, body), nil
		}
		return nil, providerPayloadError(geminiProvider, ".candidates", ErrUnsupportedProviderPayload)
	}
	if candidates.kind != jsonArray {
		return nil, providerPayloadError(geminiProvider, ".candidates", ErrUnsupportedProviderContent)
	}
	payload := newProviderPayload(geminiProvider, body)
	for index, candidate := range candidates.array {
		path := fmt.Sprintf(".candidates[%d]", index)
		if candidate.kind != jsonObject {
			return nil, providerPayloadError(geminiProvider, path, ErrUnsupportedProviderContent)
		}
		content := candidate.object["content"]
		if content == nil {
			continue
		}
		if err := appendGeminiContent(payload, "model", path+".content", content); err != nil {
			return nil, err
		}
	}
	return payload, nil
}

func appendGeminiContent(payload *ProviderPayload, role, path string, content *jsonNode) error {
	if content == nil || content.kind != jsonObject {
		return providerPayloadError(geminiProvider, path, ErrUnsupportedProviderContent)
	}
	parts := content.object["parts"]
	if parts == nil || parts.kind != jsonArray || len(parts.array) == 0 {
		return providerPayloadError(geminiProvider, path+".parts", ErrUnsupportedProviderContent)
	}
	for index, part := range parts.array {
		if err := appendGeminiPart(payload, role, fmt.Sprintf("%s.parts[%d]", path, index), part); err != nil {
			return err
		}
	}
	return nil
}

func appendGeminiPart(payload *ProviderPayload, role, path string, part *jsonNode) error {
	if part.kind != jsonObject {
		return providerPayloadError(geminiProvider, path, ErrUnsupportedProviderContent)
	}
	dataFields := []string{"text", "inlineData", "fileData", "functionCall", "functionResponse", "executableCode", "codeExecutionResult", "toolCall", "toolResponse"}
	field := ""
	for _, candidate := range dataFields {
		if part.object[candidate] != nil {
			if field != "" {
				return providerPayloadError(geminiProvider, path, ErrUnsupportedProviderContent)
			}
			field = candidate
		}
	}
	if field == "" {
		return providerPayloadError(geminiProvider, path, ErrUnsupportedProviderContent)
	}
	value := part.object[field]
	switch field {
	case "text":
		if value.kind != jsonString {
			return providerPayloadError(geminiProvider, path+".text", ErrUnsupportedProviderContent)
		}
		payload.addString(role, path+".text", value)
	case "inlineData", "fileData":
		if value.kind != jsonObject {
			return providerPayloadError(geminiProvider, path+"."+field, ErrUnsupportedProviderContent)
		}
	case "functionCall":
		return appendGeminiFunctionPayload(payload, "tool_call", path+".functionCall", value, "name", "args", false)
	case "functionResponse":
		return appendGeminiFunctionPayload(payload, "tool_result", path+".functionResponse", value, "name", "response", true)
	case "executableCode":
		return appendGeminiNestedString(payload, role, path+".executableCode", value, "code")
	case "codeExecutionResult":
		return appendGeminiOptionalNestedString(payload, "tool_result", path+".codeExecutionResult", value, "output")
	case "toolCall":
		return appendGeminiFunctionPayload(payload, "tool_call", path+".toolCall", value, "toolType", "args", false)
	case "toolResponse":
		return appendGeminiFunctionPayload(payload, "tool_result", path+".toolResponse", value, "toolType", "response", false)
	}
	return nil
}

func appendGeminiFunctionPayload(payload *ProviderPayload, role, path string, value *jsonNode, identityField, dataField string, dataRequired bool) error {
	if value.kind != jsonObject {
		return providerPayloadError(geminiProvider, path, ErrUnsupportedProviderContent)
	}
	identity, data := value.object[identityField], value.object[dataField]
	if identity == nil || identity.kind != jsonString {
		return providerPayloadError(geminiProvider, path+"."+identityField, ErrUnsupportedProviderContent)
	}
	if data == nil {
		if dataRequired {
			return providerPayloadError(geminiProvider, path+"."+dataField, ErrUnsupportedProviderContent)
		}
		return nil
	}
	if data.kind != jsonObject {
		return providerPayloadError(geminiProvider, path+"."+dataField, ErrUnsupportedProviderContent)
	}
	payload.addJSONObject(role, path+"."+dataField, data)
	return nil
}

func appendGeminiOptionalNestedString(payload *ProviderPayload, role, path string, value *jsonNode, field string) error {
	if value.kind != jsonObject {
		return providerPayloadError(geminiProvider, path, ErrUnsupportedProviderContent)
	}
	text := value.object[field]
	if text == nil {
		return nil
	}
	if text.kind != jsonString {
		return providerPayloadError(geminiProvider, path+"."+field, ErrUnsupportedProviderContent)
	}
	payload.addString(role, path+"."+field, text)
	return nil
}

func appendGeminiNestedString(payload *ProviderPayload, role, path string, value *jsonNode, field string) error {
	if value.kind != jsonObject {
		return providerPayloadError(geminiProvider, path, ErrUnsupportedProviderContent)
	}
	text := value.object[field]
	if text == nil || text.kind != jsonString {
		return providerPayloadError(geminiProvider, path+"."+field, ErrUnsupportedProviderContent)
	}
	payload.addString(role, path+"."+field, text)
	return nil
}
