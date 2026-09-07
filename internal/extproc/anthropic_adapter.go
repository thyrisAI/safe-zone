package extproc

import "fmt"

const anthropicProvider = "anthropic_messages"

func ParseAnthropicRequest(contentType string, body []byte) (*ProviderPayload, error) {
	root, err := parseProviderDocument(anthropicProvider, contentType, body)
	if err != nil {
		return nil, err
	}
	messages := root.object["messages"]
	if messages == nil {
		return nil, providerPayloadError(anthropicProvider, ".messages", ErrUnsupportedProviderPayload)
	}
	if messages.kind != jsonArray {
		return nil, providerPayloadError(anthropicProvider, ".messages", ErrUnsupportedProviderContent)
	}
	payload := newProviderPayload(anthropicProvider, body)
	if system := root.object["system"]; system != nil {
		if err := appendAnthropicSystem(payload, system); err != nil {
			return nil, err
		}
	}
	for messageIndex, message := range messages.array {
		base := fmt.Sprintf(".messages[%d]", messageIndex)
		if message.kind != jsonObject {
			return nil, providerPayloadError(anthropicProvider, base, ErrUnsupportedProviderContent)
		}
		role := message.object["role"]
		if role == nil || role.kind != jsonString || (role.stringValue != "user" && role.stringValue != "assistant") {
			return nil, providerPayloadError(anthropicProvider, base+".role", ErrUnsupportedProviderContent)
		}
		content := message.object["content"]
		if content == nil {
			return nil, providerPayloadError(anthropicProvider, base+".content", ErrUnsupportedProviderContent)
		}
		switch content.kind {
		case jsonString:
			payload.addString(role.stringValue, base+".content", content)
		case jsonArray:
			if err := appendAnthropicBlocks(payload, role.stringValue, base+".content", content, true); err != nil {
				return nil, err
			}
		default:
			return nil, providerPayloadError(anthropicProvider, base+".content", ErrUnsupportedProviderContent)
		}
	}
	return payload, nil
}

func ParseAnthropicResponse(contentType string, body []byte) (*ProviderPayload, error) {
	root, err := parseProviderDocument(anthropicProvider, contentType, body)
	if err != nil {
		return nil, err
	}
	typeNode := root.object["type"]
	if typeNode == nil || typeNode.kind != jsonString || typeNode.stringValue != "message" {
		return nil, providerPayloadError(anthropicProvider, ".type", ErrUnsupportedProviderPayload)
	}
	role := root.object["role"]
	if role == nil || role.kind != jsonString || role.stringValue != "assistant" {
		return nil, providerPayloadError(anthropicProvider, ".role", ErrUnsupportedProviderContent)
	}
	content := root.object["content"]
	if content == nil || content.kind != jsonArray {
		return nil, providerPayloadError(anthropicProvider, ".content", ErrUnsupportedProviderContent)
	}
	payload := newProviderPayload(anthropicProvider, body)
	if err := appendAnthropicBlocks(payload, "assistant", ".content", content, false); err != nil {
		return nil, err
	}
	return payload, nil
}

func appendAnthropicSystem(payload *ProviderPayload, system *jsonNode) error {
	switch system.kind {
	case jsonString:
		payload.addString("system", ".system", system)
	case jsonArray:
		if len(system.array) == 0 {
			return providerPayloadError(anthropicProvider, ".system", ErrUnsupportedProviderContent)
		}
		for index, block := range system.array {
			path := fmt.Sprintf(".system[%d]", index)
			if err := appendAnthropicTextBlock(payload, "system", path, block); err != nil {
				return err
			}
		}
	default:
		return providerPayloadError(anthropicProvider, ".system", ErrUnsupportedProviderContent)
	}
	return nil
}

func appendAnthropicBlocks(payload *ProviderPayload, role, path string, blocks *jsonNode, request bool) error {
	if len(blocks.array) == 0 {
		return providerPayloadError(anthropicProvider, path, ErrUnsupportedProviderContent)
	}
	for index, block := range blocks.array {
		blockPath := fmt.Sprintf("%s[%d]", path, index)
		if block.kind != jsonObject {
			return providerPayloadError(anthropicProvider, blockPath, ErrUnsupportedProviderContent)
		}
		blockType := block.object["type"]
		if blockType == nil || blockType.kind != jsonString {
			return providerPayloadError(anthropicProvider, blockPath+".type", ErrUnsupportedProviderContent)
		}
		switch blockType.stringValue {
		case "text":
			if err := appendAnthropicTextBlock(payload, role, blockPath, block); err != nil {
				return err
			}
		case "tool_use":
			if role != "assistant" {
				return providerPayloadError(anthropicProvider, blockPath+".type", ErrUnsupportedProviderContent)
			}
			id, name, input := block.object["id"], block.object["name"], block.object["input"]
			if id == nil || id.kind != jsonString || name == nil || name.kind != jsonString || input == nil || input.kind != jsonObject {
				return providerPayloadError(anthropicProvider, blockPath+".input", ErrUnsupportedProviderContent)
			}
			payload.addJSONObject("tool_call", blockPath+".input", input)
		case "tool_result":
			if !request || role != "user" {
				return providerPayloadError(anthropicProvider, blockPath+".type", ErrUnsupportedProviderContent)
			}
			toolUseID, content := block.object["tool_use_id"], block.object["content"]
			if toolUseID == nil || toolUseID.kind != jsonString || content == nil {
				return providerPayloadError(anthropicProvider, blockPath+".content", ErrUnsupportedProviderContent)
			}
			switch content.kind {
			case jsonString:
				payload.addString("tool_result", blockPath+".content", content)
			case jsonArray:
				if err := appendAnthropicToolResultBlocks(payload, blockPath+".content", content); err != nil {
					return err
				}
			default:
				return providerPayloadError(anthropicProvider, blockPath+".content", ErrUnsupportedProviderContent)
			}
		case "image", "document":
			if role != "user" || block.object["text"] != nil {
				return providerPayloadError(anthropicProvider, blockPath, ErrUnsupportedProviderContent)
			}
		case "thinking", "redacted_thinking":
			if role != "assistant" || block.object["text"] != nil {
				return providerPayloadError(anthropicProvider, blockPath, ErrUnsupportedProviderContent)
			}
		default:
			return providerPayloadError(anthropicProvider, blockPath+".type", ErrUnsupportedProviderContent)
		}
	}
	return nil
}

func appendAnthropicTextBlock(payload *ProviderPayload, role, path string, block *jsonNode) error {
	if block.kind != jsonObject {
		return providerPayloadError(anthropicProvider, path, ErrUnsupportedProviderContent)
	}
	blockType, text := block.object["type"], block.object["text"]
	if blockType == nil || blockType.kind != jsonString || blockType.stringValue != "text" || text == nil || text.kind != jsonString {
		return providerPayloadError(anthropicProvider, path+".text", ErrUnsupportedProviderContent)
	}
	payload.addString(role, path+".text", text)
	return nil
}

func appendAnthropicToolResultBlocks(payload *ProviderPayload, path string, blocks *jsonNode) error {
	if len(blocks.array) == 0 {
		return providerPayloadError(anthropicProvider, path, ErrUnsupportedProviderContent)
	}
	for index, block := range blocks.array {
		blockPath := fmt.Sprintf("%s[%d]", path, index)
		if block.kind != jsonObject {
			return providerPayloadError(anthropicProvider, blockPath, ErrUnsupportedProviderContent)
		}
		blockType := block.object["type"]
		if blockType == nil || blockType.kind != jsonString {
			return providerPayloadError(anthropicProvider, blockPath+".type", ErrUnsupportedProviderContent)
		}
		switch blockType.stringValue {
		case "text":
			if err := appendAnthropicTextBlock(payload, "tool_result", blockPath, block); err != nil {
				return err
			}
		case "image", "document":
			if block.object["text"] != nil {
				return providerPayloadError(anthropicProvider, blockPath, ErrUnsupportedProviderContent)
			}
		default:
			return providerPayloadError(anthropicProvider, blockPath+".type", ErrUnsupportedProviderContent)
		}
	}
	return nil
}
