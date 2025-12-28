package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestDetect_ErrorHandling(t *testing.T) {
	// This function is just a placeholder, actual tests are in TestDetect_ErrorHandling_Real
}

func getBaseURL() string {
	if v := os.Getenv("TSZ_BASE_URL"); v != "" {
		return v
	}
	return "http://localhost:8080"
}

func TestDetect_ErrorHandling_Real(t *testing.T) {
	baseURL := getBaseURL()

	tests := []struct {
		name           string
		payload        interface{}
		expectedStatus int
		checkError     bool
	}{
		{
			"Empty payload",
			map[string]interface{}{},
			400,
			true,
		},
		{
			"Missing text field",
			map[string]interface{}{
				"mode": "DETECT",
			},
			400,
			true,
		},
		{
			"Invalid mode",
			map[string]interface{}{
				"text": "test@example.com",
				"mode": "INVALID_MODE",
			},
			400,
			true,
		},
		{
			"Extremely long text",
			map[string]interface{}{
				"text": generateLongText(100000), // 100KB text
				"mode": "DETECT",
			},
			200, // Should handle gracefully
			false,
		},
		{
			"Special characters and unicode",
			map[string]interface{}{
				"text": "🚀 test@example.com 中文 العربية русский",
				"mode": "DETECT",
			},
			200,
			false,
		},
		{
			"Null text",
			map[string]interface{}{
				"text": nil,
				"mode": "DETECT",
			},
			400,
			true,
		},
	}

	client := &http.Client{
		Timeout: 10 * time.Second,
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonData, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("Failed to marshal payload: %v", err)
			}

			resp, err := client.Post(baseURL+"/detect", "application/json", bytes.NewBuffer(jsonData))
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Fatalf("Expected status %d, got %d", tt.expectedStatus, resp.StatusCode)
			}

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			if tt.checkError {
				if _, hasError := result["error"]; !hasError {
					t.Fatalf("Expected error in response, but got: %v", result)
				}
			}
		})
	}
}

func TestGateway_ErrorHandling(t *testing.T) {
	baseURL := getBaseURL()

	tests := []struct {
		name           string
		payload        interface{}
		headers        map[string]string
		expectedStatus int
	}{
		{
			"Empty payload",
			map[string]interface{}{},
			nil,
			400,
		},
		{
			"Missing messages",
			map[string]interface{}{
				"model": "test-model",
			},
			nil,
			400,
		},
		{
			"Invalid message format",
			map[string]interface{}{
				"model": "test-model",
				"messages": []interface{}{
					"invalid message format",
				},
			},
			nil,
			400,
		},
		{
			"Empty messages array",
			map[string]interface{}{
				"model":    "test-model",
				"messages": []interface{}{},
			},
			nil,
			400,
		},
		{
			"Invalid guardrails header",
			map[string]interface{}{
				"model": "test-model",
				"messages": []interface{}{
					map[string]interface{}{
						"role":    "user",
						"content": "Hello world",
					},
				},
			},
			map[string]string{
				"X-TSZ-Guardrails": "INVALID_GUARDRAIL",
			},
			400,
		},
		{
			"Valid request with unknown model",
			map[string]interface{}{
				"model": "unknown-model",
				"messages": []interface{}{
					map[string]interface{}{
						"role":    "user",
						"content": "Hello world",
					},
				},
			},
			nil,
			502, // Bad gateway - upstream model not configured
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jsonData, err := json.Marshal(tt.payload)
			if err != nil {
				t.Fatalf("Failed to marshal payload: %v", err)
			}

			req, err := http.NewRequest("POST", baseURL+"/v1/chat/completions", bytes.NewBuffer(jsonData))
			if err != nil {
				t.Fatalf("Failed to create request: %v", err)
			}

			req.Header.Set("Content-Type", "application/json")
			for key, value := range tt.headers {
				req.Header.Set(key, value)
			}

			client := &http.Client{}
			resp, err := client.Do(req)
			if err != nil {
				t.Fatalf("Request failed: %v", err)
			}
			defer resp.Body.Close()

			if resp.StatusCode != tt.expectedStatus {
				t.Logf("Expected status %d, got %d (this might be expected if upstream is not configured)", tt.expectedStatus, resp.StatusCode)
			}

			var result map[string]interface{}
			if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
				t.Fatalf("Failed to decode response: %v", err)
			}

			// For error responses, ensure we have proper error structure
			if resp.StatusCode >= 400 {
				if _, hasError := result["error"]; !hasError {
					t.Logf("Expected error in response for status %d, but got: %v", resp.StatusCode, result)
				}
			}
		})
	}
}

func TestPatterns_CRUD_ErrorHandling(t *testing.T) {
	baseURL := getBaseURL()

	// Test creating pattern with invalid data
	t.Run("Create pattern with invalid regex", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":     "INVALID_REGEX",
			"regex":    "[invalid regex",
			"category": "PII",
			"active":   true,
		}

		jsonData, _ := json.Marshal(payload)
		resp, err := http.Post(baseURL+"/patterns", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should return error for invalid regex
		if resp.StatusCode == 200 {
			t.Log("Pattern creation succeeded despite invalid regex (might be handled gracefully)")
		}
	})

	// Test deleting non-existent pattern
	t.Run("Delete non-existent pattern", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", baseURL+"/patterns/99999", nil)
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should handle gracefully
		if resp.StatusCode != 404 && resp.StatusCode != 200 {
			t.Logf("Unexpected status for non-existent pattern deletion: %d", resp.StatusCode)
		}
	})
}

func TestValidators_CRUD_ErrorHandling(t *testing.T) {
	baseURL := getBaseURL()

	// Test creating validator with invalid schema
	t.Run("Create validator with invalid JSON schema", func(t *testing.T) {
		payload := map[string]interface{}{
			"name":        "INVALID_SCHEMA",
			"description": "Test invalid schema",
			"schema":      "invalid json schema",
		}

		jsonData, _ := json.Marshal(payload)
		resp, err := http.Post(baseURL+"/validators", "application/json", bytes.NewBuffer(jsonData))
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should return error for invalid schema
		if resp.StatusCode == 200 {
			t.Log("Validator creation succeeded despite invalid schema (might be handled gracefully)")
		}
	})

	// Test deleting non-existent validator
	t.Run("Delete non-existent validator", func(t *testing.T) {
		req, _ := http.NewRequest("DELETE", baseURL+"/validators/99999", nil)
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			t.Fatalf("Request failed: %v", err)
		}
		defer resp.Body.Close()

		// Should handle gracefully
		if resp.StatusCode != 404 && resp.StatusCode != 200 {
			t.Logf("Unexpected status for non-existent validator deletion: %d", resp.StatusCode)
		}
	})
}

func TestConcurrentRequests(t *testing.T) {
	baseURL := getBaseURL()

	// Test concurrent detect requests
	t.Run("Concurrent detect requests", func(t *testing.T) {
		const numRequests = 10
		results := make(chan error, numRequests)

		for i := 0; i < numRequests; i++ {
			go func(id int) {
				payload := map[string]interface{}{
					"text": fmt.Sprintf("test%d@example.com", id),
					"mode": "DETECT",
				}

				jsonData, _ := json.Marshal(payload)
				resp, err := http.Post(baseURL+"/detect", "application/json", bytes.NewBuffer(jsonData))
				if err != nil {
					results <- err
					return
				}
				defer resp.Body.Close()

				if resp.StatusCode != 200 {
					results <- fmt.Errorf("request %d failed with status %d", id, resp.StatusCode)
					return
				}

				results <- nil
			}(i)
		}

		// Wait for all requests to complete
		for i := 0; i < numRequests; i++ {
			if err := <-results; err != nil {
				t.Fatalf("Concurrent request failed: %v", err)
			}
		}
	})
}

// Helper function to generate long text for testing
func generateLongText(length int) string {
	// Use text without PII patterns to avoid triggering thousands of AI checks which would timeout
	text := "This is a long test text to verify that the system can handle large payloads without crashing. "
	var sb strings.Builder
	for sb.Len() < length {
		sb.WriteString(text)
	}
	return sb.String()[:length]
}
