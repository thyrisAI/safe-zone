package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/bedrockruntime"
)

// BedrockConfig holds configuration for the AWS Bedrock provider.
type BedrockConfig struct {
	// Region is the AWS region for Bedrock (e.g., "us-east-1", "eu-central-1").
	Region string
	// EndpointOverride is an optional custom endpoint URL (for testing or VPC endpoints).
	EndpointOverride string
	// ModelID is the Bedrock model identifier (e.g., "anthropic.claude-3-sonnet-20240229-v1:0").
	ModelID string
}

// BedrockProvider implements ChatProvider for AWS Bedrock.
type BedrockProvider struct {
	config BedrockConfig
	client *bedrockruntime.Client
}

// NewBedrockProvider creates a new AWS Bedrock provider.
func NewBedrockProvider(cfg BedrockConfig) (*BedrockProvider, error) {
	if cfg.Region == "" {
		return nil, fmt.Errorf("bedrock region is required")
	}
	if cfg.ModelID == "" {
		return nil, fmt.Errorf("bedrock model ID is required")
	}

	// Load AWS configuration using standard credential chain
	// (environment variables, shared credentials file, IAM role, etc.)
	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(cfg.Region),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	// Create Bedrock Runtime client
	clientOpts := []func(*bedrockruntime.Options){}
	if cfg.EndpointOverride != "" {
		clientOpts = append(clientOpts, func(o *bedrockruntime.Options) {
			o.BaseEndpoint = aws.String(cfg.EndpointOverride)
		})
	}

	client := bedrockruntime.NewFromConfig(awsCfg, clientOpts...)

	return &BedrockProvider{
		config: cfg,
		client: client,
	}, nil
}

// Name returns the provider name.
func (p *BedrockProvider) Name() string {
	return "bedrock"
}

// SupportsStreaming returns false for now (streaming will be added in phase 2).
func (p *BedrockProvider) SupportsStreaming() bool {
	// Bedrock supports streaming via InvokeModelWithResponseStream,
	// but we're implementing non-streaming first.
	return false
}

// Chat sends a non-streaming chat completion request to Bedrock.
func (p *BedrockProvider) Chat(ctx context.Context, req ChatRequest) (*ChatResponse, error) {
	// Determine the model family and build the appropriate request
	modelID := req.Model
	if modelID == "" {
		modelID = p.config.ModelID
	}

	// Build the request body based on the model family
	body, err := p.buildRequestBody(modelID, req)
	if err != nil {
		return nil, fmt.Errorf("failed to build request body: %w", err)
	}

	log.Printf("[bedrock] Invoking model %s", modelID)

	// Call Bedrock InvokeModel
	output, err := p.client.InvokeModel(ctx, &bedrockruntime.InvokeModelInput{
		ModelId:     aws.String(modelID),
		ContentType: aws.String("application/json"),
		Accept:      aws.String("application/json"),
		Body:        body,
	})
	if err != nil {
		log.Printf("[bedrock] InvokeModel failed: %v", err)
		return nil, fmt.Errorf("bedrock invoke failed: %w", err)
	}

	// Parse the response based on the model family
	return p.parseResponse(modelID, output.Body)
}

// ChatStream sends a streaming chat completion request to Bedrock.
// Currently returns an error as streaming is not yet implemented.
func (p *BedrockProvider) ChatStream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, <-chan error) {
	eventCh := make(chan StreamEvent)
	errCh := make(chan error, 1)

	// Close channels immediately and return error
	go func() {
		defer close(eventCh)
		defer close(errCh)
		errCh <- ErrStreamingNotSupported
	}()

	return eventCh, errCh
}

// buildRequestBody constructs the request body for the specific Bedrock model.
func (p *BedrockProvider) buildRequestBody(modelID string, req ChatRequest) ([]byte, error) {
	// Detect model family from model ID
	modelFamily := detectModelFamily(modelID)

	switch modelFamily {
	case "anthropic":
		return p.buildAnthropicRequest(req)
	case "amazon":
		return p.buildTitanRequest(req)
	case "meta":
		return p.buildLlamaRequest(req)
	case "mistral":
		return p.buildMistralRequest(req)
	case "cohere":
		return p.buildCohereRequest(req)
	default:
		// Default to Anthropic format as it's the most common
		log.Printf("[bedrock] Unknown model family for %s, using Anthropic format", modelID)
		return p.buildAnthropicRequest(req)
	}
}

// detectModelFamily determines the model family from the model ID.
func detectModelFamily(modelID string) string {
	modelID = strings.ToLower(modelID)

	switch {
	case strings.Contains(modelID, "anthropic") || strings.Contains(modelID, "claude"):
		return "anthropic"
	case strings.Contains(modelID, "amazon") || strings.Contains(modelID, "titan"):
		return "amazon"
	case strings.Contains(modelID, "meta") || strings.Contains(modelID, "llama"):
		return "meta"
	case strings.Contains(modelID, "mistral"):
		return "mistral"
	case strings.Contains(modelID, "cohere"):
		return "cohere"
	default:
		return "unknown"
	}
}

// buildAnthropicRequest builds a request body for Anthropic Claude models.
func (p *BedrockProvider) buildAnthropicRequest(req ChatRequest) ([]byte, error) {
	// Convert messages to Anthropic format
	messages := make([]map[string]interface{}, 0, len(req.Messages))
	var systemPrompt string

	for _, msg := range req.Messages {
		if msg.Role == "system" {
			systemPrompt = msg.Content
			continue
		}

		role := msg.Role
		if role == "assistant" {
			role = "assistant"
		} else {
			role = "user"
		}

		messages = append(messages, map[string]interface{}{
			"role": role,
			"content": []map[string]interface{}{
				{
					"type": "text",
					"text": msg.Content,
				},
			},
		})
	}

	body := map[string]interface{}{
		"anthropic_version": "bedrock-2023-05-31",
		"messages":          messages,
		"max_tokens":        4096,
	}

	if systemPrompt != "" {
		body["system"] = systemPrompt
	}

	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}

	return json.Marshal(body)
}

// buildTitanRequest builds a request body for Amazon Titan models.
func (p *BedrockProvider) buildTitanRequest(req ChatRequest) ([]byte, error) {
	// Combine all messages into a single prompt for Titan
	var prompt strings.Builder
	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			prompt.WriteString(msg.Content)
			prompt.WriteString("\n\n")
		case "user":
			prompt.WriteString("User: ")
			prompt.WriteString(msg.Content)
			prompt.WriteString("\n\n")
		case "assistant":
			prompt.WriteString("Assistant: ")
			prompt.WriteString(msg.Content)
			prompt.WriteString("\n\n")
		}
	}
	prompt.WriteString("Assistant: ")

	textGenConfig := map[string]interface{}{
		"maxTokenCount": 4096,
	}

	if req.MaxTokens > 0 {
		textGenConfig["maxTokenCount"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		textGenConfig["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		textGenConfig["topP"] = req.TopP
	}

	body := map[string]interface{}{
		"inputText":            prompt.String(),
		"textGenerationConfig": textGenConfig,
	}

	return json.Marshal(body)
}

// buildLlamaRequest builds a request body for Meta Llama models.
func (p *BedrockProvider) buildLlamaRequest(req ChatRequest) ([]byte, error) {
	// Build prompt in Llama chat format
	var prompt strings.Builder
	prompt.WriteString("<|begin_of_text|>")

	for _, msg := range req.Messages {
		prompt.WriteString(fmt.Sprintf("<|start_header_id|>%s<|end_header_id|>\n\n%s<|eot_id|>", msg.Role, msg.Content))
	}
	prompt.WriteString("<|start_header_id|>assistant<|end_header_id|>\n\n")

	body := map[string]interface{}{
		"prompt":      prompt.String(),
		"max_gen_len": 2048,
	}

	if req.MaxTokens > 0 {
		body["max_gen_len"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}

	return json.Marshal(body)
}

// buildMistralRequest builds a request body for Mistral models.
func (p *BedrockProvider) buildMistralRequest(req ChatRequest) ([]byte, error) {
	// Build prompt in Mistral format
	var prompt strings.Builder
	prompt.WriteString("<s>")

	for _, msg := range req.Messages {
		switch msg.Role {
		case "system":
			prompt.WriteString(fmt.Sprintf("[INST] %s [/INST]", msg.Content))
		case "user":
			prompt.WriteString(fmt.Sprintf("[INST] %s [/INST]", msg.Content))
		case "assistant":
			prompt.WriteString(msg.Content)
			prompt.WriteString("</s>")
		}
	}

	body := map[string]interface{}{
		"prompt":     prompt.String(),
		"max_tokens": 4096,
	}

	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["top_p"] = req.TopP
	}

	return json.Marshal(body)
}

// buildCohereRequest builds a request body for Cohere models.
func (p *BedrockProvider) buildCohereRequest(req ChatRequest) ([]byte, error) {
	// Extract the last user message as the main message
	var message string
	var chatHistory []map[string]string

	for i, msg := range req.Messages {
		if i == len(req.Messages)-1 && msg.Role == "user" {
			message = msg.Content
		} else {
			role := "USER"
			if msg.Role == "assistant" {
				role = "CHATBOT"
			}
			chatHistory = append(chatHistory, map[string]string{
				"role":    role,
				"message": msg.Content,
			})
		}
	}

	body := map[string]interface{}{
		"message":      message,
		"chat_history": chatHistory,
		"max_tokens":   4096,
	}

	if req.MaxTokens > 0 {
		body["max_tokens"] = req.MaxTokens
	}
	if req.Temperature > 0 {
		body["temperature"] = req.Temperature
	}
	if req.TopP > 0 {
		body["p"] = req.TopP
	}

	return json.Marshal(body)
}

// parseResponse parses the Bedrock response based on the model family.
func (p *BedrockProvider) parseResponse(modelID string, body []byte) (*ChatResponse, error) {
	modelFamily := detectModelFamily(modelID)

	var content string
	var err error

	switch modelFamily {
	case "anthropic":
		content, err = p.parseAnthropicResponse(body)
	case "amazon":
		content, err = p.parseTitanResponse(body)
	case "meta":
		content, err = p.parseLlamaResponse(body)
	case "mistral":
		content, err = p.parseMistralResponse(body)
	case "cohere":
		content, err = p.parseCohereResponse(body)
	default:
		content, err = p.parseAnthropicResponse(body)
	}

	if err != nil {
		return nil, err
	}

	// Build OpenAI-compatible response
	return &ChatResponse{
		ID:      fmt.Sprintf("bedrock-%d", time.Now().UnixNano()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   modelID,
		Choices: []ChatChoice{
			{
				Index: 0,
				Message: ChatMessage{
					Role:    "assistant",
					Content: content,
				},
				FinishReason: "stop",
			},
		},
	}, nil
}

// parseAnthropicResponse parses an Anthropic Claude response.
func (p *BedrockProvider) parseAnthropicResponse(body []byte) (string, error) {
	var resp struct {
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
		StopReason string `json:"stop_reason"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse Anthropic response: %w", err)
	}

	var content strings.Builder
	for _, c := range resp.Content {
		if c.Type == "text" {
			content.WriteString(c.Text)
		}
	}

	return content.String(), nil
}

// parseTitanResponse parses an Amazon Titan response.
func (p *BedrockProvider) parseTitanResponse(body []byte) (string, error) {
	var resp struct {
		Results []struct {
			OutputText string `json:"outputText"`
		} `json:"results"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse Titan response: %w", err)
	}

	if len(resp.Results) == 0 {
		return "", fmt.Errorf("no results in Titan response")
	}

	return resp.Results[0].OutputText, nil
}

// parseLlamaResponse parses a Meta Llama response.
func (p *BedrockProvider) parseLlamaResponse(body []byte) (string, error) {
	var resp struct {
		Generation string `json:"generation"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse Llama response: %w", err)
	}

	return resp.Generation, nil
}

// parseMistralResponse parses a Mistral response.
func (p *BedrockProvider) parseMistralResponse(body []byte) (string, error) {
	var resp struct {
		Outputs []struct {
			Text string `json:"text"`
		} `json:"outputs"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse Mistral response: %w", err)
	}

	if len(resp.Outputs) == 0 {
		return "", fmt.Errorf("no outputs in Mistral response")
	}

	return resp.Outputs[0].Text, nil
}

// parseCohereResponse parses a Cohere response.
func (p *BedrockProvider) parseCohereResponse(body []byte) (string, error) {
	var resp struct {
		Text string `json:"text"`
	}

	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("failed to parse Cohere response: %w", err)
	}

	return resp.Text, nil
}

// ForwardRequest forwards a raw OpenAI-compatible request to Bedrock.
// This converts the OpenAI format to Bedrock format and back.
func (p *BedrockProvider) ForwardRequest(ctx context.Context, payload map[string]interface{}) (*http.Response, error) {
	// Extract messages from payload
	messagesRaw, ok := payload["messages"].([]interface{})
	if !ok {
		return nil, fmt.Errorf("invalid messages in payload")
	}

	messages := make([]ChatMessage, 0, len(messagesRaw))
	for _, m := range messagesRaw {
		msgMap, ok := m.(map[string]interface{})
		if !ok {
			continue
		}
		role, _ := msgMap["role"].(string)
		content, _ := msgMap["content"].(string)
		messages = append(messages, ChatMessage{Role: role, Content: content})
	}

	// Extract model
	model, _ := payload["model"].(string)
	if model == "" {
		model = p.config.ModelID
	}

	// Build ChatRequest
	req := ChatRequest{
		Model:    model,
		Messages: messages,
	}

	if maxTokens, ok := payload["max_tokens"].(float64); ok {
		req.MaxTokens = int(maxTokens)
	}
	if temp, ok := payload["temperature"].(float64); ok {
		req.Temperature = temp
	}
	if topP, ok := payload["top_p"].(float64); ok {
		req.TopP = topP
	}

	// Call Chat
	chatResp, err := p.Chat(ctx, req)
	if err != nil {
		// Return an error response
		errBody, _ := json.Marshal(map[string]interface{}{
			"error": map[string]interface{}{
				"message": err.Error(),
				"type":    "bedrock_error",
			},
		})
		return &http.Response{
			StatusCode: http.StatusBadGateway,
			Body:       io.NopCloser(bytes.NewReader(errBody)),
			Header:     http.Header{"Content-Type": []string{"application/json"}},
		}, nil
	}

	// Convert to OpenAI format response
	respBody, err := json.Marshal(chatResp)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal response: %w", err)
	}

	return &http.Response{
		StatusCode: http.StatusOK,
		Body:       io.NopCloser(bytes.NewReader(respBody)),
		Header:     http.Header{"Content-Type": []string{"application/json"}},
	}, nil
}

// Ensure BedrockProvider implements ChatProvider
var _ ChatProvider = (*BedrockProvider)(nil)

// Ensure BedrockProvider implements OpenAIForwarder
var _ OpenAIForwarder = (*BedrockProvider)(nil)
