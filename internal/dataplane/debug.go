package dataplane

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/maximhq/bifrost/core/schemas"

	"github.com/inwinter04/omniflux/internal/asset"
	"github.com/inwinter04/omniflux/internal/bifrostx"
)

const maxDebugResponseBytes = 1024 * 1024

type DebugResult struct {
	Response        string
	Provider        string
	UpstreamModel   string
	InputTokens     int64
	OutputTokens    int64
	CachedTokens    int64
	ReasoningTokens int64
	Cost            float64
	Currency        string
	FirstTokenMS    *int
	TotalMS         int
}

type DebugStreamEvent struct {
	Delta  string
	Result *DebugResult
	Err    error
}

func (d Deps) DebugCompletion(ctx context.Context, organizationID, serviceID uuid.UUID, prompt string, outputSchema json.RawMessage) (DebugResult, error) {
	content := schemas.ChatMessageContent{ContentStr: &prompt}
	params := debugPromptParams(outputSchema)
	return d.DebugConversation(ctx, organizationID, serviceID, []schemas.ChatMessage{{Role: schemas.ChatMessageRoleUser, Content: &content}}, params)
}

func (d Deps) DebugConversation(ctx context.Context, organizationID, serviceID uuid.UUID, messages []schemas.ChatMessage, params *schemas.ChatParameters) (DebugResult, error) {
	chain, bctx, request, start, err := d.prepareConversationRequest(ctx, organizationID, serviceID, messages, params)
	if err != nil {
		return DebugResult{}, err
	}
	response, bifrostErr := d.Engine.Client().ChatCompletionRequest(bctx, request)
	if bifrostErr != nil {
		return DebugResult{}, errors.New(errorMessageOf(bifrostErr))
	}
	result := DebugResult{TotalMS: int(time.Since(start).Milliseconds())}
	result.Response = debugResponseContent(response)
	result.Provider = string(response.ExtraFields.RoutingInfo.Provider)
	result.UpstreamModel = response.ExtraFields.RoutingInfo.Model
	usage := usageFromBifrost(response.Usage)
	result.InputTokens, result.OutputTokens, result.CachedTokens, result.ReasoningTokens = usage.tokens()
	if served, err := chain.findCandidate(result.Provider, result.UpstreamModel, response.ExtraFields.RoutingInfo.Key); err == nil {
		result.Cost = served.costUSD(result.InputTokens, result.OutputTokens)
		result.Currency = currencyOf(served)
	}
	return result, nil
}

func (d Deps) DebugCompletionStream(ctx context.Context, organizationID, serviceID uuid.UUID, prompt string, outputSchema json.RawMessage) (<-chan DebugStreamEvent, error) {
	content := schemas.ChatMessageContent{ContentStr: &prompt}
	params := debugPromptParams(outputSchema)
	return d.DebugConversationStream(ctx, organizationID, serviceID, []schemas.ChatMessage{{Role: schemas.ChatMessageRoleUser, Content: &content}}, params)
}

func (d Deps) DebugConversationStream(ctx context.Context, organizationID, serviceID uuid.UUID, messages []schemas.ChatMessage, params *schemas.ChatParameters) (<-chan DebugStreamEvent, error) {
	chain, bctx, request, start, err := d.prepareConversationRequest(ctx, organizationID, serviceID, messages, params)
	if err != nil {
		return nil, err
	}
	stream, bifrostErr := d.Engine.Client().ChatCompletionStreamRequest(bctx, request)
	if bifrostErr != nil {
		return nil, errors.New(errorMessageOf(bifrostErr))
	}
	events := make(chan DebugStreamEvent, 16)
	go func() {
		defer close(events)
		result := DebugResult{}
		var content strings.Builder
		var snapshot usageSnapshot
		var servedAccountID string
		for chunk := range stream {
			if chunk.BifrostError != nil {
				sendDebugStreamEvent(ctx, events, DebugStreamEvent{Err: errors.New(errorMessageOf(chunk.BifrostError))})
				return
			}
			response := chunk.BifrostChatResponse
			if response == nil {
				continue
			}
			if result.Provider == "" {
				result.Provider = string(response.ExtraFields.RoutingInfo.Provider)
				result.UpstreamModel = response.ExtraFields.RoutingInfo.Model
				servedAccountID = response.ExtraFields.RoutingInfo.Key
			}
			if usage := usageFromBifrost(response.Usage); usage.hasTokens() {
				snapshot = usageSnapshot(usage)
			}
			for _, choice := range response.Choices {
				if choice.Delta == nil || choice.Delta.Content == nil || *choice.Delta.Content == "" {
					continue
				}
				delta := *choice.Delta.Content
				if result.FirstTokenMS == nil {
					elapsed := int(time.Since(start).Milliseconds())
					result.FirstTokenMS = &elapsed
				}
				if content.Len()+len(delta) > maxDebugResponseBytes {
					sendDebugStreamEvent(ctx, events, DebugStreamEvent{Err: errors.New("Model output exceeds the debug size limit.")})
					return
				}
				content.WriteString(delta)
				if !sendDebugStreamEvent(ctx, events, DebugStreamEvent{Delta: delta}) {
					return
				}
			}
		}
		result.Response = content.String()
		result.TotalMS = int(time.Since(start).Milliseconds())
		result.InputTokens, result.OutputTokens, result.CachedTokens, result.ReasoningTokens = snapshot.InputTokens, snapshot.OutputTokens, snapshot.CachedTokens, snapshot.ReasoningTokens
		if served, err := chain.findCandidate(result.Provider, result.UpstreamModel, servedAccountID); err == nil {
			result.Cost = served.costUSD(result.InputTokens, result.OutputTokens)
			result.Currency = currencyOf(served)
		}
		sendDebugStreamEvent(ctx, events, DebugStreamEvent{Result: &result})
	}()
	return events, nil
}

func debugPromptParams(outputSchema json.RawMessage) *schemas.ChatParameters {
	params := &schemas.ChatParameters{}
	if format, ok := asset.StructuredResponseFormat(asset.PromptSpec{OutputSchema: outputSchema}); ok {
		params.ResponseFormat = &format
	}
	return params
}

func (d Deps) prepareConversationRequest(ctx context.Context, organizationID, serviceID uuid.UUID, messages []schemas.ChatMessage, params *schemas.ChatParameters) (routeChain, *schemas.BifrostContext, *schemas.BifrostChatRequest, time.Time, error) {
	var name string
	if err := d.Pool.QueryRow(ctx, `SELECT name FROM model_services WHERE id = $1 AND organization_id = $2 AND status = 'active'`, serviceID, organizationID).Scan(&name); err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("Model service does not exist or is not enabled.")
	}
	service, found, err := d.loadService(ctx, organizationID, name)
	if err != nil || !found {
		return nil, nil, nil, time.Time{}, fmt.Errorf("Model service has no available candidates.")
	}
	scores := d.loadCandidateHealthWithBreaker(ctx, organizationID, service.Candidates, service.Policy.CircuitBreaker)
	decision := EvaluateRoute(service.Candidates, service.Policy, RouteContext{}, scores, d.fxRatesSnapshot(), 0)
	chain := decision.Chain
	if len(chain) == 0 {
		return nil, nil, nil, time.Time{}, fmt.Errorf("Model service has no eligible candidates.")
	}
	primary := chain[0]
	provider, err := bifrostx.MapProvider(primary.Provider)
	if err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	request := &schemas.BifrostChatRequest{
		Provider:  provider,
		Model:     primary.UpstreamModel,
		Input:     messages,
		Params:    params,
		Fallbacks: fallbackSpecs(chain),
	}
	start := time.Now()
	timeout := time.Duration(service.Policy.ProviderTimeoutMS) * time.Millisecond
	bctx := schemas.NewBifrostContext(ctx, time.Now().Add(timeout))
	bctx.SetValue(schemas.BifrostContextKeyAPIKeyID, primary.AccountID)
	return chain, bctx, request, start, nil
}

func debugResponseContent(response *schemas.BifrostChatResponse) string {
	if response == nil || len(response.Choices) == 0 {
		return ""
	}
	choice := response.Choices[0]
	if choice.ChatNonStreamResponseChoice == nil || choice.Message == nil || choice.Message.Content == nil || choice.Message.Content.ContentStr == nil {
		return ""
	}
	return *choice.Message.Content.ContentStr
}

func sendDebugStreamEvent(ctx context.Context, events chan<- DebugStreamEvent, event DebugStreamEvent) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}
