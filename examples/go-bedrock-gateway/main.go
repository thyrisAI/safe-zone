// Package main demonstrates using Safe Zone with AWS Bedrock as the AI provider.
//
// This example shows how to:
// 1. Configure Safe Zone to use AWS Bedrock
// 2. Send chat completion requests through the TSZ gateway
// 3. Apply guardrails to both input and output
//
// Prerequisites:
// - AWS credentials configured (via environment, shared credentials, or IAM role)
// - Access to AWS Bedrock in your region
// - Safe Zone server running with THYRIS_AI_PROVIDER=BEDROCK
//
// Usage:
//
//	# Start Safe Zone with Bedrock configuration
//	AI_PROVIDER=BEDROCK \
//	AWS_BEDROCK_REGION=us-east-1 \
//	AWS_BEDROCK_MODEL_ID=anthropic.claude-3-sonnet-20240229-v1:0 \
//	go run main.go
//
//	# Run this example
//	cd examples/go-bedrock-gateway
//	go run main.go
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
)

const (
	defaultTSZURL = "http://localhost:8080"
)

// ChatMessage represents a message in the chat conversation
type ChatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// ChatRequest represents an OpenAI-compatible chat completion request
type ChatRequest struct {
	Model    string        `json:"model"`
	Messages []ChatMessage `json:"messages"`
	Stream   bool          `json:"stream"`
}

// ChatResponse represents an OpenAI-compatible chat completion response
type ChatResponse struct {
	ID      string `json:"id"`
	Object  string `json:"object"`
	Created int64  `json:"created"`
	Model   string `json:"model"`
	Choices []struct {
		Index   int `json:"index"`
		Message struct {
			Role    string `json:"role"`
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	TSZMeta map[string]interface{} `json:"tsz_meta,omitempty"`
}

func main() {
	tszURL := os.Getenv("TSZ_URL")
	if tszURL == "" {
		tszURL = defaultTSZURL
	}

	fmt.Println("=== Safe Zone + AWS Bedrock Example ===")
	fmt.Printf("TSZ Gateway URL: %s\n\n", tszURL)

	// Example 1: Simple chat completion
	fmt.Println("--- Example 1: Simple Chat Completion ---")
	simpleChat(tszURL)

	// Example 2: Chat with PII detection
	fmt.Println("\n--- Example 2: Chat with PII Detection ---")
	chatWithPII(tszURL)

	// Example 3: Chat with guardrails
	fmt.Println("\n--- Example 3: Chat with Guardrails ---")
	chatWithGuardrails(tszURL)
}

func simpleChat(tszURL string) {
	modelID := os.Getenv("AWS_BEDROCK_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-3-sonnet-20240229-v1:0" // Default fallback
	}

	req := ChatRequest{
		Model: modelID,
		Messages: []ChatMessage{
			{Role: "user", Content: "What is the capital of France? Answer in one sentence."},
		},
		Stream: false,
	}

	resp, err := sendChatRequest(tszURL, req, nil)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	if len(resp.Choices) > 0 {
		fmt.Printf("Response: %s\n", resp.Choices[0].Message.Content)
	}
}

func chatWithPII(tszURL string) {
	modelID := os.Getenv("AWS_BEDROCK_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-3-sonnet-20240229-v1:0" // Default fallback
	}

	// This message contains PII that should be detected and masked
	req := ChatRequest{
		Model: modelID,
		Messages: []ChatMessage{
			{
				Role:    "user",
				Content: "My email is john.doe@example.com and my phone is 555-123-4567. Can you help me?",
			},
		},
		Stream: false,
	}

	resp, err := sendChatRequest(tszURL, req, nil)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	if len(resp.Choices) > 0 {
		fmt.Printf("Response: %s\n", resp.Choices[0].Message.Content)
	}

	// Check TSZ metadata for detection results
	if resp.TSZMeta != nil {
		fmt.Printf("TSZ Metadata: %+v\n", resp.TSZMeta)
	}
}

func chatWithGuardrails(tszURL string) {
	modelID := os.Getenv("AWS_BEDROCK_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-3-sonnet-20240229-v1:0" // Default fallback
	}

	req := ChatRequest{
		Model: modelID,
		Messages: []ChatMessage{
			{Role: "system", Content: "You are a helpful assistant."},
			{Role: "user", Content: "Tell me a short joke."},
		},
		Stream: false,
	}

	// Add guardrails header
	headers := map[string]string{
		"X-TSZ-Guardrails": "TOXIC_LANGUAGE,PROMPT_INJECTION",
		"X-TSZ-RID":        "bedrock-example-001",
	}

	resp, err := sendChatRequest(tszURL, req, headers)
	if err != nil {
		log.Printf("Error: %v\n", err)
		return
	}

	if len(resp.Choices) > 0 {
		fmt.Printf("Response: %s\n", resp.Choices[0].Message.Content)
	}

	// Check TSZ metadata
	if resp.TSZMeta != nil {
		if guardrails, ok := resp.TSZMeta["guardrails"].([]interface{}); ok && len(guardrails) > 0 {
			fmt.Printf("Triggered Guardrails: %v\n", guardrails)
		}
	}
}

func sendChatRequest(tszURL string, req ChatRequest, headers map[string]string) (*ChatResponse, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal request: %w", err)
	}

	httpReq, err := http.NewRequest("POST", tszURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	httpReq.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		httpReq.Header.Set(k, v)
	}

	client := &http.Client{}
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer httpResp.Body.Close()

	respBody, err := io.ReadAll(httpResp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response: %w", err)
	}

	if httpResp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("request failed with status %d: %s", httpResp.StatusCode, string(respBody))
	}

	var chatResp ChatResponse
	if err := json.Unmarshal(respBody, &chatResp); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &chatResp, nil
}
