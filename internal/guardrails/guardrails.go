package guardrails

import (
	"fmt"
	"log"
	"os"
	"regexp"
	"strings"
	"sync"
	"thyris-sz/internal/ai"
	"thyris-sz/internal/models"
	"thyris-sz/internal/repository"
	"time"
)

// Detector handles PII detection and redaction
type Detector struct{}

var regexCache sync.Map

// getCachedRegex retrieves a compiled regex from cache or compiles it
func getCachedRegex(pattern string) (*regexp.Regexp, error) {
	if v, ok := regexCache.Load(pattern); ok {
		return v.(*regexp.Regexp), nil
	}
	r, err := regexp.Compile(pattern)
	if err != nil {
		return nil, err
	}
	regexCache.Store(pattern, r)
	return r, nil
}

// resolveAction maps confidence score to action
func resolveAction(score float64, allowThreshold float64, blockThreshold float64) string {
	// Safety check for invalid thresholds
	if allowThreshold > blockThreshold {
		return "MASK"
	}
	if score >= blockThreshold {
		return "BLOCK"
	}
	if score < allowThreshold {
		return "ALLOW"
	}
	return "MASK"
}

// NewDetector creates a new instance of Detector
func NewDetector() *Detector {
	if count, err := repository.CountActivePatterns(); err != nil {
		log.Printf("Failed to count patterns: %v", err)
	} else {
		log.Printf("Detector initialized with %d active patterns from database", count)
	}
	return &Detector{}
}

// Detect scans the input text for PII and returns redacted text and detections
func (d *Detector) Detect(req models.DetectRequest) models.DetectResponse {
	var blocked bool
	var messages []string

	// 0. Guardrails / Validators Execution
	// Collect unique validators to run
	validatorsToRun := make(map[string]bool)
	if req.ExpectedFormat != "" {
		validatorsToRun[req.ExpectedFormat] = true
	}
	for _, g := range req.Guardrails {
		validatorsToRun[g] = true
	}

	var validatorResults []models.ValidatorResult
	for vName := range validatorsToRun {
		validator, _ := repository.GetValidatorByName(vName)
		valid, err := ValidateFormat(req.Text, vName)
		confidence := 0.5

		// AI validators get higher, model-based confidence baseline
		if validator != nil && validator.Type == "AI_PROMPT" {
			confidence = 0.85
		}
		if err != nil {
			confidence = 1.0
			log.Printf("Validator error [%s]: %v", vName, err)
			blocked = true
			messages = append(messages, fmt.Sprintf("Error in guardrail '%s': %v", vName, err))
		} else if !valid {
			confidence = 0.9
			blocked = true
			messages = append(messages, fmt.Sprintf("Content blocked by security policy: %s", vName))
		} else {
			confidence = 0.7
		}

		validatorResults = append(validatorResults, models.ValidatorResult{
			Name:            vName,
			Type:            "VALIDATOR",
			Passed:          valid && err == nil,
			ConfidenceScore: models.Confidence(roundConfidence(confidence)),
		})
	}

	// Note: We continue to PII detection even if blocked by guardrails,
	// to provide full visibility as requested.

	var candidates []models.DetectionResult
	redactedText := req.Text

	dbPatterns, err := repository.GetActivePatterns()
	if err != nil {
		log.Printf("Error fetching patterns: %v", err)
		return models.DetectResponse{RedactedText: req.Text}
	}

	allowlistMap, err := repository.GetAllowlistMap()
	if err != nil {
		log.Printf("Error fetching allowlist: %v", err)
		allowlistMap = make(map[string]bool)
	}

	blocklistMap, err := repository.GetBlocklistMap()
	if err != nil {
		log.Printf("Error fetching blocklist: %v", err)
		blocklistMap = make(map[string]bool)
	}

	// 1. Scan Blocklist (Exact/Contains match)
	for badWord := range blocklistMap {
		// Simple Contains check (case-sensitive? usually blocklists are insensitive, but map keys are likely as-is)
		// For better performance/accuracy, we should compile these into a regex, but for now iterate.
		if strings.Contains(req.Text, badWord) {
			// Find all occurrences
			// Note: strings.Index only finds first. regexp is better.
			// Let's use a simple regex for the literal string to find all indices.
			// escaped := regexp.QuoteMeta(badWord) // Unused for now
			// Word boundary? The user asked for "blacklist", usually implies keywords.
			// Let's enforce word boundary for better quality \bWORD\b
			// But for Turkish/Unicode, \b might be tricky. Let's use simple literal search for now to be safe.
			// actually literal search might match "scunthorpe".
			// Let's stick to simple strings.Contains and find all.

			// Finding all indices of substring
			searchStr := req.Text
			offset := 0
			for {
				idx := strings.Index(searchStr, badWord)
				if idx == -1 {
					break
				}
				realStart := offset + idx
				realEnd := realStart + len(badWord)

				candidates = append(candidates, models.DetectionResult{
					Type:        "BLOCKLIST",
					Value:       badWord,
					Placeholder: "[BLOCKED]", // Or use generatesPlaceholder
					Start:       realStart,
					End:         realEnd,
				})

				// Move search forward
				searchStr = searchStr[idx+len(badWord):]
				offset = realEnd
			}
		}
	}

	// 2. Find all candidates (Patterns)
	for _, p := range dbPatterns {
		regex, err := getCachedRegex(p.Regex)
		if err != nil {
			log.Printf("Invalid regex for pattern %s: %v", p.Name, err)
			continue
		}

		matches := regex.FindAllStringIndex(req.Text, -1)
		for _, match := range matches {
			value := req.Text[match[0]:match[1]]

			if allowlistMap[value] {
				continue
			}

			placeholder := generatePlaceholder(p.Name, req.RID)

			ctx := ConfidenceContext{
				PatternCategory: p.Category,
				PatternActive:   p.IsActive,
				AllowlistHit:    false,
				BlacklistHit:    false,
				Source:          "REGEX",
			}

			regexScore := ComputeConfidence(ctx)
			finalConfidence := regexScore
			var aiScore float64

			// Hybrid PII confidence: refine with AI micro-confidence
			if p.Category == "PII" {
				if v, err := ai.ConfidenceWithAI(value, p.Name); err == nil {
					aiScore = v
					finalConfidence = (regexScore + v) / 2
				}
			}

			explanation := &models.ConfidenceExplanation{
				Source:        "HYBRID",
				RegexScore:    models.Confidence(roundConfidence(regexScore)),
				Category:      p.Category,
				PatternActive: p.IsActive,
				FinalScore:    models.Confidence(roundConfidence(finalConfidence)),
			}

			if aiScore > 0 {
				explanation.AIScore = models.Confidence(roundConfidence(aiScore))
			}

			candidates = append(candidates, models.DetectionResult{
				Type:                  p.Name,
				Value:                 value,
				Placeholder:           placeholder,
				Start:                 match[0],
				End:                   match[1],
				ConfidenceScore:       models.Confidence(roundConfidence(finalConfidence)),
				ConfidenceExplanation: explanation,
			})
		}
	}

	// 3. Sort candidates by Start index ASC, then by End index DESC (Longest match wins)
	if len(candidates) > 0 {
		for i := 1; i < len(candidates); i++ {
			j := i
			for j > 0 {
				shouldSwap := false
				if candidates[j-1].Start > candidates[j].Start {
					shouldSwap = true
				} else if candidates[j-1].Start == candidates[j].Start {
					// If starts match, prefer longer match (larger End index)
					if candidates[j-1].End < candidates[j].End {
						shouldSwap = true
					}
				}

				if shouldSwap {
					candidates[j-1], candidates[j] = candidates[j], candidates[j-1]
					j = j - 1
				} else {
					break
				}
			}
		}
	}

	// 4. Filter overlaps and build final list
	var detections []models.DetectionResult
	currentIndex := 0
	for _, d := range candidates {
		if d.Start < currentIndex {
			// Overlap detected, skip this candidate
			continue
		}
		detections = append(detections, d)
		currentIndex = d.End
	}

	// 5. Calculate Breakdown from valid detections
	breakdown := make(map[string]int)
	for _, d := range detections {
		breakdown[d.Type]++
	}

	mode := req.Mode
	if mode == "" {
		mode = os.Getenv("PII_MODE")
		if mode == "" {
			mode = "MASK"
		}
	}

	containsPII := len(detections) > 0

	// Confidence-based action mapping (enterprise)
	blockThreshold := getBlockThreshold()
	allowThreshold := getAllowThreshold()

	for _, d := range detections {
		score := float64(d.ConfidenceScore)
		action := resolveAction(score, allowThreshold, blockThreshold)

		// Publish security event
		publishSecurityEvent(models.SecurityEvent{
			Type:            action,
			Action:          action,
			Category:        d.Type,
			Pattern:         d.Type,
			ConfidenceScore: score,
			Threshold:       blockThreshold,
			RequestID:       req.RID,
			Timestamp:       time.Now().Unix(),
		})

		switch action {
		case "BLOCK":
			blocked = true
			messages = append(messages, "Blocked due to high confidence detection: "+d.Type)
		case "ALLOW":
			// noop
		case "MASK":
			// masking handled later
		}
	}

	// Fallback BLOCK mode
	if mode == "BLOCK" && containsPII {
		blocked = true
		messages = append(messages, "PII detected, request blocked by mode.")
	}

	// MASK Mode
	// Even if blocked, we might want to show redacted text?
	// Usually if blocked, we don't return redacted text or we return it partially.
	// Here we simply perform redaction logic if PII exists.
	if containsPII {
		var result []byte
		currentIndex = 0
		for _, d := range detections {
			result = append(result, req.Text[currentIndex:d.Start]...)
			result = append(result, d.Placeholder...)
			currentIndex = d.End
		}
		if currentIndex < len(req.Text) {
			result = append(result, req.Text[currentIndex:]...)
		}
		redactedText = string(result)
	}

	finalMessage := ""
	if len(messages) > 0 {
		finalMessage = strings.Join(messages, "; ")
	}

	// 6. Compute overall confidence (weighted)
	overall := 0.0
	weight := 0.0

	for _, d := range detections {
		w := 1.0
		if d.Type == "BLOCKLIST" {
			w = 2.0
		}
		overall += float64(d.ConfidenceScore) * w
		weight += w
	}

	for _, v := range validatorResults {
		overall += float64(v.ConfidenceScore) * 1.5
		weight += 1.5
	}

	if weight > 0 {
		overall = overall / weight
	}

	return models.DetectResponse{
		RedactedText:      redactedText,
		Detections:        detections,
		ValidatorResults:  validatorResults,
		Breakdown:         breakdown,
		Blocked:           blocked,
		ContainsPII:       containsPII,
		OverallConfidence: models.Confidence(roundConfidence(overall)),
		Message:           finalMessage,
	}
}
