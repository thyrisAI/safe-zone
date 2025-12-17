//go:build bedrock
// +build bedrock

// Package integration contains integration tests for the TSZ gateway.
// This file contains tests for AWS Bedrock integration.
//
// To run these tests, you need:
// 1. Valid AWS credentials configured (via environment, shared credentials, or IAM role)
// 2. Access to AWS Bedrock in the configured region
// 3. The bedrock build tag: go test -tags=bedrock ./tests/integration/...
//
// Example:
//
//	AWS_REGION=us-east-1 go test -tags=bedrock -v ./tests/integration/ -run TestBedrock
package integration

import (
	"context"
	"os"
	"testing"
	"time"

	"thyris-sz/internal/ai"
	"thyris-sz/internal/config"
)

func TestBedrockProviderInitialization(t *testing.T) {
	// Skip if no region is configured
	region := os.Getenv("AWS_BEDROCK_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		t.Skip("Skipping Bedrock test: no AWS region configured (set AWS_BEDROCK_REGION or AWS_REGION)")
	}

	modelID := os.Getenv("AWS_BEDROCK_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-3-sonnet-20240229-v1:0"
	}

	provider, err := ai.NewBedrockProvider(ai.BedrockConfig{
		Region:  region,
		ModelID: modelID,
	})
	if err != nil {
		t.Fatalf("Failed to create Bedrock provider: %v", err)
	}

	if provider.Name() != "bedrock" {
		t.Errorf("Expected provider name 'bedrock', got '%s'", provider.Name())
	}

	if provider.SupportsStreaming() {
		t.Error("Expected SupportsStreaming() to return false for initial implementation")
	}
}

func TestBedrockChatCompletion(t *testing.T) {
	// Skip if no region is configured
	region := os.Getenv("AWS_BEDROCK_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		t.Skip("Skipping Bedrock test: no AWS region configured (set AWS_BEDROCK_REGION or AWS_REGION)")
	}

	modelID := os.Getenv("AWS_BEDROCK_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-3-sonnet-20240229-v1:0"
	}

	provider, err := ai.NewBedrockProvider(ai.BedrockConfig{
		Region:  region,
		ModelID: modelID,
	})
	if err != nil {
		t.Fatalf("Failed to create Bedrock provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := ai.ChatRequest{
		Messages: []ai.ChatMessage{
			{Role: "user", Content: "Say 'Hello, World!' and nothing else."},
		},
		MaxTokens: 50,
	}

	resp, err := provider.Chat(ctx, req)
	if err != nil {
		t.Fatalf("Chat request failed: %v", err)
	}

	if resp == nil {
		t.Fatal("Response is nil")
	}

	if len(resp.Choices) == 0 {
		t.Fatal("No choices in response")
	}

	content := resp.Choices[0].Message.Content
	if content == "" {
		t.Error("Empty content in response")
	}

	t.Logf("Bedrock response: %s", content)
}

func TestBedrockForwardRequest(t *testing.T) {
	// Skip if no region is configured
	region := os.Getenv("AWS_BEDROCK_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		t.Skip("Skipping Bedrock test: no AWS region configured (set AWS_BEDROCK_REGION or AWS_REGION)")
	}

	modelID := os.Getenv("AWS_BEDROCK_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-3-sonnet-20240229-v1:0"
	}

	provider, err := ai.NewBedrockProvider(ai.BedrockConfig{
		Region:  region,
		ModelID: modelID,
	})
	if err != nil {
		t.Fatalf("Failed to create Bedrock provider: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Test OpenAI-compatible payload forwarding
	payload := map[string]interface{}{
		"model": modelID,
		"messages": []interface{}{
			map[string]interface{}{
				"role":    "user",
				"content": "What is 2+2? Answer with just the number.",
			},
		},
		"max_tokens": 10,
	}

	resp, err := provider.ForwardRequest(ctx, payload)
	if err != nil {
		t.Fatalf("ForwardRequest failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		t.Errorf("Expected status 200, got %d", resp.StatusCode)
	}
}

func TestBedrockProviderViaConfig(t *testing.T) {
	// Skip if no region is configured
	region := os.Getenv("AWS_BEDROCK_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		t.Skip("Skipping Bedrock test: no AWS region configured (set AWS_BEDROCK_REGION or AWS_REGION)")
	}

	// Set up config for Bedrock
	originalProvider := os.Getenv("AI_PROVIDER")
	originalRegion := os.Getenv("AWS_BEDROCK_REGION")
	originalModelID := os.Getenv("AWS_BEDROCK_MODEL_ID")

	os.Setenv("AI_PROVIDER", "BEDROCK")
	os.Setenv("AWS_BEDROCK_REGION", region)
	if originalModelID == "" {
		os.Setenv("AWS_BEDROCK_MODEL_ID", "anthropic.claude-3-sonnet-20240229-v1:0")
	}

	defer func() {
		if originalProvider != "" {
			os.Setenv("AI_PROVIDER", originalProvider)
		} else {
			os.Unsetenv("AI_PROVIDER")
		}
		if originalRegion != "" {
			os.Setenv("AWS_BEDROCK_REGION", originalRegion)
		} else {
			os.Unsetenv("AWS_BEDROCK_REGION")
		}
		if originalModelID != "" {
			os.Setenv("AWS_BEDROCK_MODEL_ID", originalModelID)
		} else {
			os.Unsetenv("AWS_BEDROCK_MODEL_ID")
		}
	}()

	// Reload config
	config.LoadConfig()

	// Initialize provider
	err := ai.InitProvider()
	if err != nil {
		t.Fatalf("Failed to initialize provider: %v", err)
	}

	provider := ai.GetProvider()
	if provider == nil {
		t.Fatal("Provider is nil after initialization")
	}

	if provider.Name() != "bedrock" {
		t.Errorf("Expected provider name 'bedrock', got '%s'", provider.Name())
	}
}

func TestBedrockStreamingNotSupported(t *testing.T) {
	// Skip if no region is configured
	region := os.Getenv("AWS_BEDROCK_REGION")
	if region == "" {
		region = os.Getenv("AWS_REGION")
	}
	if region == "" {
		t.Skip("Skipping Bedrock test: no AWS region configured (set AWS_BEDROCK_REGION or AWS_REGION)")
	}

	modelID := os.Getenv("AWS_BEDROCK_MODEL_ID")
	if modelID == "" {
		modelID = "anthropic.claude-3-sonnet-20240229-v1:0"
	}

	provider, err := ai.NewBedrockProvider(ai.BedrockConfig{
		Region:  region,
		ModelID: modelID,
	})
	if err != nil {
		t.Fatalf("Failed to create Bedrock provider: %v", err)
	}

	ctx := context.Background()
	req := ai.ChatRequest{
		Messages: []ai.ChatMessage{
			{Role: "user", Content: "Hello"},
		},
		Stream: true,
	}

	eventCh, errCh := provider.ChatStream(ctx, req)

	// Should receive an error
	select {
	case err := <-errCh:
		if err != ai.ErrStreamingNotSupported {
			t.Errorf("Expected ErrStreamingNotSupported, got: %v", err)
		}
	case <-eventCh:
		t.Error("Did not expect to receive events")
	case <-time.After(time.Second):
		t.Error("Timeout waiting for error")
	}
}
