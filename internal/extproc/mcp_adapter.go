package extproc

import "fmt"

const mcpProvider = "mcp_jsonrpc"

// ParseMCPRequest extracts user-controlled prompt arguments and tool-call
// arguments from one buffered MCP Streamable HTTP JSON-RPC message. Other MCP
// methods contain no prompt/tool payload covered by this adapter and pass
// through with an empty content list.
func ParseMCPRequest(contentType string, body []byte) (*ProviderPayload, error) {
	root, err := parseMCPDocument(contentType, body)
	if err != nil {
		return nil, err
	}
	method := root.object["method"]
	if method == nil || method.kind != jsonString {
		return nil, providerPayloadError(mcpProvider, ".method", ErrUnsupportedProviderContent)
	}
	payload := newProviderPayload(mcpProvider, body)
	if method.stringValue != "prompts/get" && method.stringValue != "tools/call" {
		return payload, nil
	}
	params := root.object["params"]
	if params == nil || params.kind != jsonObject || params.duplicateKeys {
		return nil, providerPayloadError(mcpProvider, ".params", ErrUnsupportedProviderContent)
	}
	name := params.object["name"]
	if name == nil || name.kind != jsonString || name.stringValue == "" {
		return nil, providerPayloadError(mcpProvider, ".params.name", ErrUnsupportedProviderContent)
	}
	arguments := params.object["arguments"]
	if arguments == nil {
		return payload, nil
	}
	if arguments.kind != jsonObject || hasDuplicateJSONKeys(arguments) {
		return nil, providerPayloadError(mcpProvider, ".params.arguments", ErrUnsupportedProviderContent)
	}
	if method.stringValue == "prompts/get" {
		for _, argument := range arguments.object {
			if argument.kind != jsonString {
				return nil, providerPayloadError(mcpProvider, ".params.arguments", ErrUnsupportedProviderContent)
			}
		}
	}
	payload.addJSONObject("user", ".params.arguments", arguments)
	return payload, nil
}

// ParseMCPResponse extracts prompt messages and tool results. Protocol errors
// and responses to unrelated MCP methods pass through unchanged.
func ParseMCPResponse(contentType string, body []byte, method string) (*ProviderPayload, error) {
	root, err := parseMCPDocument(contentType, body)
	if err != nil {
		return nil, err
	}
	payload := newProviderPayload(mcpProvider, body)
	if method != "prompts/get" && method != "tools/call" {
		return payload, nil
	}
	result := root.object["result"]
	if result == nil {
		if root.object["error"] != nil {
			return payload, nil
		}
		return nil, providerPayloadError(mcpProvider, ".result", ErrUnsupportedProviderContent)
	}
	if result.kind != jsonObject || result.duplicateKeys {
		return nil, providerPayloadError(mcpProvider, ".result", ErrUnsupportedProviderContent)
	}
	messages, content := result.object["messages"], result.object["content"]
	structured := result.object["structuredContent"]
	if method == "prompts/get" {
		if messages == nil || content != nil || structured != nil {
			return nil, providerPayloadError(mcpProvider, ".result.messages", ErrUnsupportedProviderContent)
		}
		if messages.kind != jsonArray || len(messages.array) == 0 {
			return nil, providerPayloadError(mcpProvider, ".result.messages", ErrUnsupportedProviderContent)
		}
		for index, message := range messages.array {
			path := fmt.Sprintf(".result.messages[%d]", index)
			if err := appendMCPPromptMessage(payload, path, message); err != nil {
				return nil, err
			}
		}
		return payload, nil
	}
	if messages != nil || content == nil || content.kind != jsonArray {
		return nil, providerPayloadError(mcpProvider, ".result.content", ErrUnsupportedProviderContent)
	}
	for index, item := range content.array {
		if err := appendMCPContent(payload, "tool_result", fmt.Sprintf(".result.content[%d]", index), item, true); err != nil {
			return nil, err
		}
	}
	if structured != nil {
		if structured.kind != jsonObject || hasDuplicateJSONKeys(structured) {
			return nil, providerPayloadError(mcpProvider, ".result.structuredContent", ErrUnsupportedProviderContent)
		}
		payload.addJSONObject("tool_result", ".result.structuredContent", structured)
	}
	return payload, nil
}

func parseMCPDocument(contentType string, body []byte) (*jsonNode, error) {
	root, err := parseProviderDocument(mcpProvider, contentType, body)
	if err != nil {
		return nil, err
	}
	version := root.object["jsonrpc"]
	if version == nil || version.kind != jsonString || version.stringValue != "2.0" {
		return nil, providerPayloadError(mcpProvider, ".jsonrpc", ErrUnsupportedProviderPayload)
	}
	if root.duplicateKeys {
		return nil, providerPayloadError(mcpProvider, "", ErrUnsupportedProviderContent)
	}
	return root, nil
}

func appendMCPPromptMessage(payload *ProviderPayload, path string, message *jsonNode) error {
	if message.kind != jsonObject || message.duplicateKeys {
		return providerPayloadError(mcpProvider, path, ErrUnsupportedProviderContent)
	}
	role, content := message.object["role"], message.object["content"]
	if role == nil || role.kind != jsonString || (role.stringValue != "user" && role.stringValue != "assistant") {
		return providerPayloadError(mcpProvider, path+".role", ErrUnsupportedProviderContent)
	}
	return appendMCPContent(payload, role.stringValue, path+".content", content, false)
}

func appendMCPContent(payload *ProviderPayload, role, path string, content *jsonNode, allowResourceLink bool) error {
	if content == nil || content.kind != jsonObject || content.duplicateKeys {
		return providerPayloadError(mcpProvider, path, ErrUnsupportedProviderContent)
	}
	typeNode := content.object["type"]
	if typeNode == nil || typeNode.kind != jsonString {
		return providerPayloadError(mcpProvider, path+".type", ErrUnsupportedProviderContent)
	}
	switch typeNode.stringValue {
	case "text":
		text := content.object["text"]
		if text == nil || text.kind != jsonString {
			return providerPayloadError(mcpProvider, path+".text", ErrUnsupportedProviderContent)
		}
		payload.addString(role, path+".text", text)
	case "resource":
		resource := content.object["resource"]
		if resource == nil || resource.kind != jsonObject || resource.duplicateKeys {
			return providerPayloadError(mcpProvider, path+".resource", ErrUnsupportedProviderContent)
		}
		uri := resource.object["uri"]
		if uri == nil || uri.kind != jsonString || uri.stringValue == "" {
			return providerPayloadError(mcpProvider, path+".resource.uri", ErrUnsupportedProviderContent)
		}
		text, blob := resource.object["text"], resource.object["blob"]
		if (text == nil) == (blob == nil) {
			return providerPayloadError(mcpProvider, path+".resource", ErrUnsupportedProviderContent)
		}
		if text != nil {
			if text.kind != jsonString {
				return providerPayloadError(mcpProvider, path+".resource.text", ErrUnsupportedProviderContent)
			}
			payload.addString(role, path+".resource.text", text)
		} else if blob.kind != jsonString {
			return providerPayloadError(mcpProvider, path+".resource.blob", ErrUnsupportedProviderContent)
		}
	case "image", "audio":
		data, mimeType := content.object["data"], content.object["mimeType"]
		if data == nil || data.kind != jsonString || mimeType == nil || mimeType.kind != jsonString {
			return providerPayloadError(mcpProvider, path+".data", ErrUnsupportedProviderContent)
		}
	case "resource_link":
		if !allowResourceLink {
			return providerPayloadError(mcpProvider, path+".type", ErrUnsupportedProviderContent)
		}
		uri, name := content.object["uri"], content.object["name"]
		if uri == nil || uri.kind != jsonString || name == nil || name.kind != jsonString {
			return providerPayloadError(mcpProvider, path+".uri", ErrUnsupportedProviderContent)
		}
	default:
		return providerPayloadError(mcpProvider, path+".type", ErrUnsupportedProviderContent)
	}
	return nil
}

func isMCPMessage(contentType string, body []byte) bool {
	root, err := parseProviderDocument(mcpProvider, contentType, body)
	if err != nil {
		return false
	}
	version := root.object["jsonrpc"]
	return version != nil && version.kind == jsonString && version.stringValue == "2.0"
}

// MCPMethodFromMessage returns the JSON-RPC method for transport adapters that
// need to correlate a response with its originating request.
func MCPMethodFromMessage(contentType string, body []byte) string {
	if !isMCPMessage(contentType, body) {
		return ""
	}
	root, err := parseProviderDocument(mcpProvider, contentType, body)
	if err != nil {
		return ""
	}
	method := root.object["method"]
	if method == nil || method.kind != jsonString {
		return ""
	}
	return method.stringValue
}

func hasDuplicateJSONKeys(node *jsonNode) bool {
	if node == nil {
		return false
	}
	if node.duplicateKeys {
		return true
	}
	for _, child := range node.object {
		if hasDuplicateJSONKeys(child) {
			return true
		}
	}
	for _, child := range node.array {
		if hasDuplicateJSONKeys(child) {
			return true
		}
	}
	return false
}
