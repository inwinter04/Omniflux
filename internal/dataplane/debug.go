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
	FirstTokenMS    *int
	TotalMS         int
}

type DebugStreamEvent struct {
	Delta  string
	Result *DebugResult
	Err    error
}

func (d Deps) DebugCompletion(ctx context.Context, organizationID, serviceID uuid.UUID, prompt string, outputSchema json.RawMessage) (DebugResult, error) {
	chain, bctx, request, start, err := d.prepareDebugRequest(ctx, organizationID, serviceID, prompt, outputSchema)
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
	if served, err := chain.findCandidate(result.Provider, result.UpstreamModel); err == nil {
		result.Cost = served.costUSD(result.InputTokens, result.OutputTokens)
	}
	return result, nil
}

func (d Deps) DebugCompletionStream(ctx context.Context, organizationID, serviceID uuid.UUID, prompt string, outputSchema json.RawMessage) (<-chan DebugStreamEvent, error) {
	chain, bctx, request, start, err := d.prepareDebugRequest(ctx, organizationID, serviceID, prompt, outputSchema)
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
					sendDebugStreamEvent(ctx, events, DebugStreamEvent{Err: errors.New("模型输出超过调试大小限制")})
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
		if served, err := chain.findCandidate(result.Provider, result.UpstreamModel); err == nil {
			result.Cost = served.costUSD(result.InputTokens, result.OutputTokens)
		}
		sendDebugStreamEvent(ctx, events, DebugStreamEvent{Result: &result})
	}()
	return events, nil
}

func (d Deps) prepareDebugRequest(ctx context.Context, organizationID, serviceID uuid.UUID, prompt string, outputSchema json.RawMessage) (routeChain, *schemas.BifrostContext, *schemas.BifrostChatRequest, time.Time, error) {
	var name string
	if err := d.Pool.QueryRow(ctx, `SELECT name FROM model_services WHERE id = $1 AND organization_id = $2 AND status = 'active'`, serviceID, organizationID).Scan(&name); err != nil {
		return nil, nil, nil, time.Time{}, fmt.Errorf("模型服务不存在或未启用")
	}
	service, found, err := d.loadService(ctx, organizationID, name)
	if err != nil || !found {
		return nil, nil, nil, time.Time{}, fmt.Errorf("模型服务没有可用候选")
	}
	scores := d.loadCandidateHealth(ctx, organizationID, service.Candidates)
	chain := selectChain(service.Candidates, service.Policy, scores)
	primary := chain[0]
	provider, err := bifrostx.MapProvider(primary.Provider)
	if err != nil {
		return nil, nil, nil, time.Time{}, err
	}
	params := &schemas.ChatParameters{}
	if format, ok := asset.StructuredResponseFormat(asset.PromptSpec{OutputSchema: outputSchema}); ok {
		params.ResponseFormat = &format
	}
	content := schemas.ChatMessageContent{ContentStr: &prompt}
	request := &schemas.BifrostChatRequest{
		Provider:  provider,
		Model:     primary.UpstreamModel,
		Input:     []schemas.ChatMessage{{Role: schemas.ChatMessageRoleUser, Content: &content}},
		Params:    params,
		Fallbacks: fallbackSpecs(chain),
	}
	start := time.Now()
	timeout := time.Duration(service.Policy.ProviderTimeoutMS) * time.Millisecond
	bctx := schemas.NewBifrostContext(ctx, time.Now().Add(timeout))
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
