package main

import (
	"context"
	"fmt"
	"log"
	"time"

	tszclient "thyris-sz/pkg/tszclient-go"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	client, err := tszclient.New(tszclient.Config{
		BaseURL: "http://localhost:8080",
	})
	if err != nil {
		log.Fatalf("failed to create tsz client: %v", err)
	}

	resp, err := client.ChatCompletions(ctx, tszclient.ChatCompletionRequest{
		Model: "llama3.1:8b",
		Messages: []map[string]interface{}{
			{"role": "user", "content": "Hello via TSZ gateway"},
		},
		Stream: false,
	}, map[string]string{
		"X-TSZ-RID":        "RID-GW-GO-001",
		"X-TSZ-Guardrails": "TOXIC_LANGUAGE",
	})
	if err != nil {
		log.Fatalf("chat completions failed: %v", err)
	}

	choices, ok := resp["choices"].([]interface{})
	if !ok || len(choices) == 0 {
		log.Println("no choices in response")
		return
	}

	first, _ := choices[0].(map[string]interface{})
	msg, _ := first["message"].(map[string]interface{})
	content, _ := msg["content"].(string)

	fmt.Println("LLM response via TSZ:")
	fmt.Println(content)
}
