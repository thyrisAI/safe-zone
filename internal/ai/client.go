package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"strings"
	"thyris-sz/internal/config"
)

// CheckWithAI sends a prompt to the configured AI model and expects a boolean-like response
func CheckWithAI(ctx context.Context, text string, promptTemplate string, expectedResponse string) (bool, error) {
	// Replace placeholder in template with actual text
	// We assume the template has {{TEXT}} placeholder or simply appends the text
	finalPrompt := promptTemplate
	if strings.Contains(promptTemplate, "{{TEXT}}") {
		finalPrompt = strings.ReplaceAll(promptTemplate, "{{TEXT}}", text)
	} else {
		finalPrompt = promptTemplate + "\n\nText to analyze:\n" + text
	}

	// Note: We do not add hardcoded instructions here anymore.
	// The promptTemplate itself should contain the instruction (e.g. "Respond 1 for YES").

	reqBody, err := json.Marshal(map[string]interface{}{
		"model": config.AppConfig.AIModelName,
		"messages": []map[string]string{
			{
				"role":    "user",
				"content": finalPrompt,
			},
		},
		"stream": false,
	})
	if err != nil {
		return false, err
	}

	url := config.AppConfig.AIModelURL + "/chat/completions"
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(reqBody))
	if err != nil {
		return false, err
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+config.AppConfig.AIAPIKey)

	// The context owns normal cancellation. This timeout is only a backstop
	// should a transport fail to observe that cancellation.
	client := &http.Client{Timeout: 2 * config.DefaultExtProcProcessingTimeout}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("AI Service connection error: %v", err)
		return false, errors.New("failed to connect to AI service")
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		bodyBytes, _ := io.ReadAll(resp.Body)
		log.Printf("AI Service returned error: %s - %s", resp.Status, string(bodyBytes))
		return false, errors.New("AI service returned non-200 status")
	}

	var aiResp struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}

	if err := json.NewDecoder(resp.Body).Decode(&aiResp); err != nil {
		return false, err
	}

	if len(aiResp.Choices) == 0 {
		return false, errors.New("no response from AI")
	}

	content := strings.TrimSpace(strings.ToLower(aiResp.Choices[0].Message.Content))

	// Default expectation if not provided
	target := expectedResponse
	if target == "" {
		target = "YES"
	}
	target = strings.ToUpper(strings.TrimSpace(target))
	contentUpper := strings.ToUpper(content)

	// Dynamic Check: Does content match (or start with) the expected valid response?
	if contentUpper == target || strings.HasPrefix(contentUpper, target) {
		return true, nil
	}

	return false, nil
}
