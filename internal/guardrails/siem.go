package guardrails

import (
	"bytes"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"thyris-sz/internal/models"
)

// publishSecurityEvent sends event to configured webhook / SIEM
func publishSecurityEvent(event models.SecurityEvent) {
	endpoint := os.Getenv("SIEM_WEBHOOK_URL")
	if endpoint == "" {
		return // disabled
	}

	payload, _ := json.Marshal(event)

	req, err := http.NewRequest("POST", endpoint, bytes.NewBuffer(payload))
	if err != nil {
		log.Printf("SIEM publish error: %v", err)
		return
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("SIEM delivery failed: %v", err)
		return
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
}
