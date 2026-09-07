package extproc

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"thyris-sz/internal/extproc/policy"
	"thyris-sz/internal/guardrails"
)

// Processor owns gateway-neutral processing logic. Transport adapters must not
// implement policy or guardrail decisions.
type Processor interface {
	Process(ctx context.Context, request ProcessingRequest) (ProcessingResult, error)
}

// StreamingWindowProcessor is implemented by processors that can evaluate a
// complete, event-aligned OpenAI SSE window and return event-level mutations.
// Keeping this optional preserves the gateway-neutral Processor contract for
// adapters that do not support streaming.
type StreamingWindowProcessor interface {
	ProcessSSEWindow(ctx context.Context, request ProcessingRequest, events []OpenAISSEEvent) (ProcessingResult, []OpenAISSEEvent, error)
}

// AllowProcessor is the Phase 1 default. It validates the contract stage and
// permits request and response messages without mutation.
type AllowProcessor struct{}

func NewAllowProcessor() Processor {
	return AllowProcessor{}
}

func (AllowProcessor) Process(ctx context.Context, request ProcessingRequest) (ProcessingResult, error) {
	if err := ctx.Err(); err != nil {
		return ProcessingResult{}, err
	}
	if err := request.Stage.Validate(); err != nil {
		return ProcessingResult{}, fmt.Errorf("allow processor: %w", err)
	}
	return ProcessingResult{Action: ActionAllow}, nil
}

// OpenAIRequestProcessor applies a stream-pinned policy to supported provider
// request and non-streaming response content. The historical name is retained
// for compatibility. It never performs floating policy lookups.
type OpenAIRequestProcessor struct {
	service guardrails.GuardrailService
}

func NewOpenAIRequestProcessor(service guardrails.GuardrailService) (*OpenAIRequestProcessor, error) {
	if service == nil {
		return nil, fmt.Errorf("guardrail service is required")
	}
	return &OpenAIRequestProcessor{service: service}, nil
}

func (p *OpenAIRequestProcessor) Process(ctx context.Context, request ProcessingRequest) (ProcessingResult, error) {
	if err := ctx.Err(); err != nil {
		return ProcessingResult{}, err
	}
	if err := request.Stage.Validate(); err != nil {
		return ProcessingResult{}, err
	}
	// Headers, empty bodies and routes without a loaded snapshot are protocol
	// ALLOW paths. Buffered Chat Completions and Responses API bodies are
	// enforced here; streaming continues through the optional window processor.
	if request.Body == nil || request.PolicySnapshot == nil {
		return ProcessingResult{Action: ActionAllow}, nil
	}
	switch request.Stage {
	case StageRequest:
		return p.processRequest(ctx, request)
	case StageResponse:
		if !request.PolicySnapshot.Definition.Response.Enabled {
			return ProcessingResult{Action: ActionAllow}, nil
		}
		return p.processResponse(ctx, request)
	default:
		return ProcessingResult{}, fmt.Errorf("unsupported processing stage %q", request.Stage)
	}
}

func (p *OpenAIRequestProcessor) ProcessSSEWindow(ctx context.Context, request ProcessingRequest, events []OpenAISSEEvent) (ProcessingResult, []OpenAISSEEvent, error) {
	if err := ctx.Err(); err != nil {
		return ProcessingResult{}, nil, err
	}
	if request.PolicySnapshot == nil || !request.PolicySnapshot.Definition.Response.Enabled {
		return ProcessingResult{Action: ActionAllow}, append([]OpenAISSEEvent(nil), events...), nil
	}
	rules, err := compiledResponseGuardrailRules(*request.PolicySnapshot)
	if err != nil {
		return ProcessingResult{}, nil, err
	}
	result := ProcessingResult{Action: ActionAllow}
	mutated := append([]OpenAISSEEvent(nil), events...)
	started := time.Now()
	// Inspect the entire event-aligned window as one text value. This is what
	// makes the trailing, not-yet-emitted overlap effective for a pattern or
	// PII value split across two SSE events.
	var joined strings.Builder
	type location struct{ event, content int }
	locations := make([]location, 0)
	for eventIndex, event := range events {
		for contentIndex, content := range event.DeltaContents {
			joined.WriteString(content)
			locations = append(locations, location{eventIndex, contentIndex})
		}
	}
	if len(locations) == 0 {
		return result, mutated, nil
	}
	inspection, err := p.inspectWithTrace(ctx, request, rules, joined.String())
	if err != nil {
		return ProcessingResult{}, nil, fmt.Errorf("inspect streaming assistant window: %w", err)
	}
	action, err := actionFromGuardrail(inspection.Action)
	if err != nil {
		return ProcessingResult{}, nil, err
	}
	result.Action = action
	result.DetectionCount = inspection.DetectionCount
	result.Metadata.Categories = append([]string(nil), inspection.Categories...)
	sort.Strings(result.Metadata.Categories)
	if action == ActionMask {
		// A cross-event match cannot safely be mapped back to its individual
		// source offsets. Emit the sanitized window content once and clear the
		// remaining deltas, preserving the client-visible concatenated text.
		contents := make([][]string, len(events))
		for index, event := range events {
			contents[index] = append([]string(nil), event.DeltaContents...)
		}
		contents[locations[0].event][locations[0].content] = inspection.SafeContent
		for _, location := range locations[1:] {
			contents[location.event][location.content] = ""
		}
		for eventIndex, event := range events {
			if len(event.DeltaContents) == 0 {
				continue
			}
			rewritten, rewriteErr := event.WithDeltaContents(contents[eventIndex])
			if rewriteErr != nil {
				return ProcessingResult{}, nil, rewriteErr
			}
			mutated[eventIndex] = rewritten
		}
	}
	result.Metadata = SafeMetadata{RequestID: request.EnvoyReqID, RID: request.RID, PolicyID: request.PolicyID, PolicyVersion: request.PolicyVersion, Adapter: "openai_chat_completions", Stage: StageResponse, Action: result.Action, Categories: result.Metadata.Categories, DetectionCount: result.DetectionCount, ProcessorLatencyMS: time.Since(started).Milliseconds()}
	if result.Action == ActionBlock {
		result.ImmediateStatus = 403
	}
	return result, mutated, nil
}

func (p *OpenAIRequestProcessor) processRequest(ctx context.Context, request ProcessingRequest) (ProcessingResult, error) {
	if isMCPMessage(request.ContentType, request.Body) {
		payload, err := ParseMCPRequest(request.ContentType, request.Body)
		if err != nil {
			return ProcessingResult{}, err
		}
		return p.processProviderPayload(ctx, request, payload, false)
	}
	// Responses also accepts string input, so select embeddings by endpoint
	// before attempting body-shape detection for other content adapters.
	if isEmbeddingsRequest(request) {
		payload, err := ParseEmbeddingsRequest(request.ContentType, request.Body)
		if err != nil {
			return ProcessingResult{}, err
		}
		return p.processProviderPayload(ctx, request, payload, false)
	}
	if isAnthropicMessagesRequest(request) {
		anthropic, err := ParseAnthropicRequest(request.ContentType, request.Body)
		if err != nil {
			return ProcessingResult{}, err
		}
		return p.processProviderPayload(ctx, request, anthropic, false)
	}
	chat, err := ParseChatRequest(request.ContentType, request.Body)
	if err == nil {
		return p.processChatRequest(ctx, request, chat)
	}
	if !errors.Is(err, ErrUnsupportedChatRequest) {
		return ProcessingResult{}, err
	}
	responses, responsesErr := ParseResponsesRequest(request.ContentType, request.Body)
	if responsesErr == nil {
		return p.processResponsesRequest(ctx, request, responses)
	}
	if !errors.Is(responsesErr, ErrUnsupportedResponsesPayload) {
		return ProcessingResult{}, responsesErr
	}
	gemini, geminiErr := ParseGeminiRequest(request.ContentType, request.Body)
	if geminiErr != nil {
		if errors.Is(geminiErr, ErrUnsupportedProviderPayload) {
			return ProcessingResult{}, responsesErr
		}
		return ProcessingResult{}, geminiErr
	}
	return p.processProviderPayload(ctx, request, gemini, false)
}

func isAnthropicMessagesRequest(request ProcessingRequest) bool {
	if FirstHeader(request.Headers, "anthropic-version") != "" {
		return true
	}
	parser := jsonSourceParser{source: request.Body}
	root, err := parser.parseDocument()
	return err == nil && root.kind == jsonObject && root.object["anthropic_version"] != nil
}

func (p *OpenAIRequestProcessor) processChatRequest(ctx context.Context, request ProcessingRequest, chat *ChatRequest) (ProcessingResult, error) {
	rules, err := compiledGuardrailRules(*request.PolicySnapshot)
	if err != nil {
		return ProcessingResult{}, err
	}
	result := ProcessingResult{Action: ActionAllow}
	categorySet := make(map[string]struct{})
	mutations := make([]ChatContentMutation, 0)
	started := time.Now()
	for _, content := range chat.Contents {
		inspection, err := p.inspectWithTrace(ctx, request, rules, content.Content)
		if err != nil {
			return ProcessingResult{}, fmt.Errorf("inspect %s message %d: %w", content.Role, content.MessageIndex, err)
		}
		action, err := actionFromGuardrail(inspection.Action)
		if err != nil {
			return ProcessingResult{}, err
		}
		result.Action = strongerProcessingAction(result.Action, action)
		result.DetectionCount += inspection.DetectionCount
		for _, category := range inspection.Categories {
			categorySet[category] = struct{}{}
		}
		if action == ActionMask {
			mutations = append(mutations, ChatContentMutation{ID: content.ID, Content: inspection.SafeContent})
		}
	}
	result.Metadata = SafeMetadata{
		RequestID: request.EnvoyReqID, RID: request.RID, PolicyID: request.PolicyID,
		PolicyVersion: request.PolicyVersion, Adapter: "openai_chat_completions", Stage: StageRequest,
		Action: result.Action, DetectionCount: result.DetectionCount,
		ProcessorLatencyMS: time.Since(started).Milliseconds(),
	}
	for category := range categorySet {
		result.Metadata.Categories = append(result.Metadata.Categories, category)
	}
	sort.Strings(result.Metadata.Categories)
	// A block prevents upstream forwarding; body mutation is unnecessary. Audit
	// only deliberately leaves the body byte-for-byte unchanged.
	if result.Action != ActionMask || len(mutations) == 0 {
		return result, nil
	}
	body, err := chat.Mutate(mutations)
	if err != nil {
		return ProcessingResult{}, err
	}
	result.Body = body
	result.HeaderMutations = map[string]string{"content-length": strconv.Itoa(len(body))}
	return result, nil
}

func (p *OpenAIRequestProcessor) processResponse(ctx context.Context, request ProcessingRequest) (ProcessingResult, error) {
	if isMCPMessage(request.ContentType, request.Body) && request.RPCMethod != "" {
		payload, err := ParseMCPResponse(request.ContentType, request.Body, request.RPCMethod)
		if err != nil {
			return ProcessingResult{}, err
		}
		return p.processProviderPayload(ctx, request, payload, true)
	}
	if isEmbeddingsRequest(request) {
		// Embeddings support is input-only. Preserve vectors and provider error
		// responses; neither is assistant text requiring output inspection.
		result := ProcessingResult{Action: ActionAllow}
		result.Metadata = providerResultMetadata(request, embeddingsProvider, result, nil, time.Now())
		return result, nil
	}
	chat, err := ParseChatResponse(request.ContentType, request.Body)
	if err == nil {
		return p.processChatResponse(ctx, request, chat)
	}
	if !errors.Is(err, ErrUnsupportedChatResponse) {
		return ProcessingResult{}, err
	}
	responses, responsesErr := ParseResponsesResponse(request.ContentType, request.Body)
	if responsesErr == nil {
		return p.processResponsesResponse(ctx, request, responses)
	}
	if !errors.Is(responsesErr, ErrUnsupportedResponsesPayload) {
		return ProcessingResult{}, responsesErr
	}
	anthropic, anthropicErr := ParseAnthropicResponse(request.ContentType, request.Body)
	if anthropicErr == nil {
		return p.processProviderPayload(ctx, request, anthropic, true)
	}
	if !errors.Is(anthropicErr, ErrUnsupportedProviderPayload) {
		return ProcessingResult{}, anthropicErr
	}
	gemini, geminiErr := ParseGeminiResponse(request.ContentType, request.Body)
	if geminiErr != nil {
		if errors.Is(geminiErr, ErrUnsupportedProviderPayload) {
			return ProcessingResult{}, responsesErr
		}
		return ProcessingResult{}, geminiErr
	}
	return p.processProviderPayload(ctx, request, gemini, true)
}

func (p *OpenAIRequestProcessor) processChatResponse(ctx context.Context, request ProcessingRequest, chat *ChatResponse) (ProcessingResult, error) {
	rules, err := compiledResponseGuardrailRules(*request.PolicySnapshot)
	if err != nil {
		return ProcessingResult{}, err
	}
	result := ProcessingResult{Action: ActionAllow}
	categorySet := make(map[string]struct{})
	mutations := make([]ChatResponseContentMutation, 0)
	started := time.Now()
	for _, content := range chat.AssistantContents {
		inspection, err := p.inspectWithTrace(ctx, request, rules, content.Content)
		if err != nil {
			return ProcessingResult{}, fmt.Errorf("inspect response content %s: %w", content.JSONPath, err)
		}
		action, err := actionFromGuardrail(inspection.Action)
		if err != nil {
			return ProcessingResult{}, err
		}
		result.Action = strongerProcessingAction(result.Action, action)
		result.DetectionCount += inspection.DetectionCount
		for _, category := range inspection.Categories {
			categorySet[category] = struct{}{}
		}
		if action == ActionMask {
			mutations = append(mutations, ChatResponseContentMutation{ID: content.ID, Content: inspection.SafeContent})
		}
	}
	result.Metadata = SafeMetadata{
		RequestID: request.EnvoyReqID, RID: request.RID, PolicyID: request.PolicyID,
		PolicyVersion: request.PolicyVersion, Adapter: "openai_chat_completions", Stage: StageResponse,
		Action: result.Action, DetectionCount: result.DetectionCount,
		ProcessorLatencyMS: time.Since(started).Milliseconds(),
	}
	for category := range categorySet {
		result.Metadata.Categories = append(result.Metadata.Categories, category)
	}
	sort.Strings(result.Metadata.Categories)
	// A block replaces the upstream response with a safe local response in the
	// Envoy adapter. Audit only leaves the response byte-for-byte unchanged.
	if result.Action != ActionMask || len(mutations) == 0 {
		if result.Action == ActionBlock {
			result.ImmediateStatus = 403
		}
		return result, nil
	}
	body, err := chat.Mutate(mutations)
	if err != nil {
		return ProcessingResult{}, err
	}
	result.Body = body
	result.HeaderMutations = map[string]string{"content-length": strconv.Itoa(len(body))}
	return result, nil
}

func (p *OpenAIRequestProcessor) processResponsesRequest(ctx context.Context, request ProcessingRequest, responses *ResponsesRequest) (ProcessingResult, error) {
	rules, err := compiledGuardrailRules(*request.PolicySnapshot)
	if err != nil {
		return ProcessingResult{}, err
	}
	result := ProcessingResult{Action: ActionAllow}
	categorySet := make(map[string]struct{})
	mutations := make([]ResponsesContentMutation, 0)
	started := time.Now()
	for _, content := range responses.Contents {
		inspection, err := p.inspectWithTrace(ctx, request, rules, content.Content)
		if err != nil {
			return ProcessingResult{}, fmt.Errorf("inspect Responses API input %s: %w", content.JSONPath, err)
		}
		action, err := actionFromGuardrail(inspection.Action)
		if err != nil {
			return ProcessingResult{}, err
		}
		result.Action = strongerProcessingAction(result.Action, action)
		result.DetectionCount += inspection.DetectionCount
		for _, category := range inspection.Categories {
			categorySet[category] = struct{}{}
		}
		if action == ActionMask {
			mutations = append(mutations, ResponsesContentMutation{ID: content.ID, Content: inspection.SafeContent})
		}
	}
	result.Metadata = providerResultMetadata(request, "openai_responses", result, categorySet, started)
	if result.Action != ActionMask || len(mutations) == 0 {
		return result, nil
	}
	body, err := responses.Mutate(mutations)
	if err != nil {
		return ProcessingResult{}, err
	}
	result.Body = body
	result.HeaderMutations = map[string]string{"content-length": strconv.Itoa(len(body))}
	return result, nil
}

func (p *OpenAIRequestProcessor) processResponsesResponse(ctx context.Context, request ProcessingRequest, responses *ResponsesResponse) (ProcessingResult, error) {
	rules, err := compiledResponseGuardrailRules(*request.PolicySnapshot)
	if err != nil {
		return ProcessingResult{}, err
	}
	result := ProcessingResult{Action: ActionAllow}
	categorySet := make(map[string]struct{})
	mutations := make([]ResponsesContentMutation, 0)
	started := time.Now()
	for _, content := range responses.Contents {
		inspection, err := p.inspectWithTrace(ctx, request, rules, content.Content)
		if err != nil {
			return ProcessingResult{}, fmt.Errorf("inspect Responses API output %s: %w", content.JSONPath, err)
		}
		action, err := actionFromGuardrail(inspection.Action)
		if err != nil {
			return ProcessingResult{}, err
		}
		result.Action = strongerProcessingAction(result.Action, action)
		result.DetectionCount += inspection.DetectionCount
		for _, category := range inspection.Categories {
			categorySet[category] = struct{}{}
		}
		if action == ActionMask {
			mutations = append(mutations, ResponsesContentMutation{ID: content.ID, Content: inspection.SafeContent})
		}
	}
	result.Metadata = providerResultMetadata(request, "openai_responses", result, categorySet, started)
	if result.Action != ActionMask || len(mutations) == 0 {
		if result.Action == ActionBlock {
			result.ImmediateStatus = 403
		}
		return result, nil
	}
	body, err := responses.Mutate(mutations)
	if err != nil {
		return ProcessingResult{}, err
	}
	result.Body = body
	result.HeaderMutations = map[string]string{"content-length": strconv.Itoa(len(body))}
	return result, nil
}

func (p *OpenAIRequestProcessor) processProviderPayload(ctx context.Context, request ProcessingRequest, payload *ProviderPayload, response bool) (ProcessingResult, error) {
	var rules *guardrails.CompiledPolicyRules
	var err error
	if response {
		rules, err = compiledResponseGuardrailRules(*request.PolicySnapshot)
	} else {
		rules, err = compiledGuardrailRules(*request.PolicySnapshot)
	}
	if err != nil {
		return ProcessingResult{}, err
	}
	result := ProcessingResult{Action: ActionAllow}
	categories := make(map[string]struct{})
	mutations := make([]ProviderContentMutation, 0)
	started := time.Now()
	for _, content := range payload.Contents {
		inspection, inspectErr := p.inspectWithTrace(ctx, request, rules, content.Content)
		if inspectErr != nil {
			return ProcessingResult{}, fmt.Errorf("inspect %s content %s: %w", payload.Provider, content.JSONPath, inspectErr)
		}
		action, actionErr := actionFromGuardrail(inspection.Action)
		if actionErr != nil {
			return ProcessingResult{}, actionErr
		}
		result.Action = strongerProcessingAction(result.Action, action)
		result.DetectionCount += inspection.DetectionCount
		for _, category := range inspection.Categories {
			categories[category] = struct{}{}
		}
		if action == ActionMask {
			mutations = append(mutations, ProviderContentMutation{ID: content.ID, Content: inspection.SafeContent})
		}
	}
	result.Metadata = providerResultMetadata(request, payload.Provider, result, categories, started)
	if result.Action != ActionMask || len(mutations) == 0 {
		if response && result.Action == ActionBlock {
			result.ImmediateStatus = 403
		}
		return result, nil
	}
	body, err := payload.Mutate(mutations)
	if err != nil {
		return ProcessingResult{}, err
	}
	result.Body = body
	result.HeaderMutations = map[string]string{"content-length": strconv.Itoa(len(body))}
	return result, nil
}

func providerResultMetadata(request ProcessingRequest, adapter string, result ProcessingResult, categories map[string]struct{}, started time.Time) SafeMetadata {
	metadata := SafeMetadata{
		RequestID: request.EnvoyReqID, RID: request.RID, PolicyID: request.PolicyID,
		PolicyVersion: request.PolicyVersion, Adapter: adapter, Stage: request.Stage,
		Action: result.Action, DetectionCount: result.DetectionCount,
		ProcessorLatencyMS: time.Since(started).Milliseconds(),
	}
	for category := range categories {
		metadata.Categories = append(metadata.Categories, category)
	}
	sort.Strings(metadata.Categories)
	return metadata
}

// inspectWithTrace creates a latency span around deterministic and semantic
// validation without ever recording the inspected text, request body, or PII.
func (p *OpenAIRequestProcessor) inspectWithTrace(ctx context.Context, request ProcessingRequest, rules *guardrails.CompiledPolicyRules, text string) (guardrails.InspectResult, error) {
	ctx, span := otel.Tracer("thyris-sz/guardrails").Start(ctx, "tsz.guardrail.validator")
	defer span.End()
	span.SetAttributes(
		attribute.String("tsz.stage", string(request.Stage)),
		attribute.String("tsz.policy.id", request.PolicyID),
		attribute.Int("tsz.policy.version", request.PolicyVersion),
	)
	result, err := p.service.Inspect(ctx, guardrails.InspectInput{Text: text, RID: request.RID, Policy: rules})
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "guardrail inspection failed")
		return guardrails.InspectResult{}, err
	}
	span.SetAttributes(attribute.Int("tsz.detection_count", result.DetectionCount))
	return result, nil
}

func compiledGuardrailRules(snapshot policy.CompiledSnapshot) (*guardrails.CompiledPolicyRules, error) {
	definition := snapshot.Definition.Request
	rules := &guardrails.CompiledPolicyRules{
		PolicyID: snapshot.PolicyID, Version: snapshot.Version,
		PIIAction:             guardrails.RuleAction(definition.PII),
		SecretAction:          guardrails.RuleAction(definition.Secret),
		PromptInjectionAction: guardrails.RuleAction(definition.PromptInjection),
	}
	for _, rule := range definition.CompiledRules.CustomPatterns {
		rules.CustomPatterns = append(rules.CustomPatterns, guardrails.CompiledPatternRule{
			ID: rule.ID, Name: rule.Name, Category: rule.Category, Regex: rule.Regex, Action: guardrails.RuleAction(rule.Action),
		})
	}
	for _, rule := range definition.CompiledRules.Allowlist {
		rules.Allowlist = append(rules.Allowlist, guardrails.CompiledListRule{ID: rule.ID, Value: rule.Value})
	}
	for _, rule := range definition.CompiledRules.Blocklist {
		rules.Blocklist = append(rules.Blocklist, guardrails.CompiledListRule{ID: rule.ID, Value: rule.Value})
	}
	for _, rule := range definition.CompiledRules.Validators {
		rules.Validators = append(rules.Validators, guardrails.CompiledValidatorRule{
			ID: rule.ID, Version: rule.Version, Name: rule.Name, Kind: guardrails.ValidatorKind(rule.Kind),
			Rule: rule.Rule, ExpectedResponse: rule.ExpectedResponse, Action: guardrails.RuleAction(rule.Action),
		})
	}
	return rules, nil
}

func compiledResponseGuardrailRules(snapshot policy.CompiledSnapshot) (*guardrails.CompiledPolicyRules, error) {
	definition := snapshot.Definition.Response
	rules := &guardrails.CompiledPolicyRules{
		PolicyID: snapshot.PolicyID, Version: snapshot.Version,
		PIIAction:             guardrails.RuleAction(definition.PII),
		SecretAction:          guardrails.RuleAction(definition.Secret),
		PromptInjectionAction: guardrails.RuleAction(definition.UnsafeContent),
	}
	for _, rule := range definition.CompiledRules.CustomPatterns {
		rules.CustomPatterns = append(rules.CustomPatterns, guardrails.CompiledPatternRule{
			ID: rule.ID, Name: rule.Name, Category: rule.Category, Regex: rule.Regex, Action: guardrails.RuleAction(rule.Action),
		})
	}
	for _, rule := range definition.CompiledRules.Validators {
		rules.Validators = append(rules.Validators, guardrails.CompiledValidatorRule{
			ID: rule.ID, Version: rule.Version, Name: rule.Name, Kind: guardrails.ValidatorKind(rule.Kind),
			Rule: rule.Rule, ExpectedResponse: rule.ExpectedResponse, Action: guardrails.RuleAction(rule.Action),
		})
	}
	return rules, nil
}

func actionFromGuardrail(action guardrails.RuleAction) (Action, error) {
	converted := Action(action)
	if err := converted.Validate(); err != nil {
		return "", fmt.Errorf("guardrail action %q: %w", action, err)
	}
	return converted, nil
}

func strongerProcessingAction(current, candidate Action) Action {
	priority := map[Action]int{ActionAllow: 0, ActionAuditOnly: 1, ActionMask: 2, ActionBlock: 3}
	if priority[candidate] > priority[current] {
		return candidate
	}
	return current
}
