package dataplane

import (
	"strings"

	"github.com/maximhq/bifrost/core/schemas"
)

// responsesStreamNormalizer repairs synthesized Chat-to-Responses and non-standard
// upstream reasoning events so strict OpenAI Responses clients (such as codex-rs)
// receive schema-compliant reasoning items, paired lifecycle events, and strictly
// monotonic sequence_number values without dropping thinking blocks or stalling UI updates.
type responsesStreamNormalizer struct {
	seq          int
	summaryMode  string
	reasoning    reasoningBlockState
}

type reasoningBlockState struct {
	open               bool
	itemID             string
	outputIndex        int
	textBuf            strings.Builder
	summaryBuf         strings.Builder
	emittedRawText     bool
	sawRawTextDone     bool
	emittedSummaryPart bool
	sawSummaryPartDone bool
	sawSummaryTextDone bool
}

func newResponsesStreamNormalizer(reasoningSummaryMode string) *responsesStreamNormalizer {
	return &responsesStreamNormalizer{
		summaryMode: strings.ToLower(strings.TrimSpace(reasoningSummaryMode)),
	}
}

func (r *reasoningBlockState) reset(itemID string, outputIndex int) {
	r.open = true
	r.itemID = itemID
	r.outputIndex = outputIndex
	r.textBuf.Reset()
	r.summaryBuf.Reset()
	r.emittedRawText = false
	r.sawRawTextDone = false
	r.emittedSummaryPart = false
	r.sawSummaryPartDone = false
	r.sawSummaryTextDone = false
}

// normalizeResponsesReasoningItem ensures a Responses "reasoning" output item omits any
// bogus "role" field (e.g. from Chat fallbacks), guarantees a valid non-nil "summary" slice
// (serializing as [] when empty), and preserves any raw reasoning content blocks.
func normalizeResponsesReasoningItem(item *schemas.ResponsesMessage, defaultStatus string, summaryText string) *schemas.ResponsesMessage {
	if item == nil {
		return nil
	}
	itemCopy := *item
	itemCopy.Role = nil
	if (itemCopy.Status == nil || *itemCopy.Status == "") && defaultStatus != "" {
		itemCopy.Status = stringPtr(defaultStatus)
	}
	if itemCopy.Content != nil && itemCopy.Content.ContentStr == nil && itemCopy.Content.ContentBlocks == nil {
		contentCopy := *itemCopy.Content
		contentCopy.ContentBlocks = []schemas.ResponsesMessageContentBlock{}
		itemCopy.Content = &contentCopy
	}

	var reasoningCopy schemas.ResponsesReasoning
	if itemCopy.ResponsesReasoning != nil {
		reasoningCopy = *itemCopy.ResponsesReasoning
		if reasoningCopy.Summary != nil {
			reasoningCopy.Summary = append([]schemas.ResponsesReasoningSummary(nil), reasoningCopy.Summary...)
		}
	}
	if reasoningCopy.Summary == nil {
		reasoningCopy.Summary = []schemas.ResponsesReasoningSummary{}
	}
	if summaryText != "" && len(reasoningCopy.Summary) == 0 {
		reasoningCopy.Summary = []schemas.ResponsesReasoningSummary{
			{
				Type: schemas.ResponsesReasoningContentBlockTypeSummaryText,
				Text: summaryText,
			},
		}
	}
	itemCopy.ResponsesReasoning = &reasoningCopy
	return &itemCopy
}

// normalizeResponsesOutputItems repairs reasoning items in a non-streaming or terminal
// Responses payload so callers receive compliant "summary": [] and no bogus "role".
func normalizeResponsesOutputItems(resp *schemas.BifrostResponsesResponse) {
	normalizeResponsesOutputItemsWithSummary(resp, "")
}

func normalizeResponsesOutputItemsWithSummary(resp *schemas.BifrostResponsesResponse, summaryText string) {
	if resp == nil || len(resp.Output) == 0 {
		return
	}
	for i := range resp.Output {
		item := &resp.Output[i]
		if item.Type != nil && *item.Type == schemas.ResponsesMessageTypeReasoning {
			resp.Output[i] = *normalizeResponsesReasoningItem(item, "completed", summaryText)
		}
	}
}

func cloneResponsesStreamEvent(ev *schemas.BifrostResponsesStreamResponse) *schemas.BifrostResponsesStreamResponse {
	if ev == nil {
		return nil
	}
	copied := *ev
	return &copied
}

func summaryPartBlock(text string) *schemas.ResponsesMessageContentBlock {
	t := text
	return &schemas.ResponsesMessageContentBlock{
		Type: schemas.ResponsesOutputMessageContentTypeSummaryText,
		Text: &t,
	}
}

func (n *responsesStreamNormalizer) nextEvent(ev *schemas.BifrostResponsesStreamResponse) *schemas.BifrostResponsesStreamResponse {
	ev.SequenceNumber = n.seq
	n.seq++
	return ev
}

func (n *responsesStreamNormalizer) resolveItemMeta(ev *schemas.BifrostResponsesStreamResponse) (string, int) {
	itemID := n.reasoning.itemID
	if ev.ItemID != nil && *ev.ItemID != "" {
		itemID = *ev.ItemID
	}
	outIdx := n.reasoning.outputIndex
	if ev.OutputIndex != nil {
		outIdx = *ev.OutputIndex
	}
	return itemID, outIdx
}

func isReasoningItem(item *schemas.ResponsesMessage) bool {
	return item != nil && item.Type != nil && *item.Type == schemas.ResponsesMessageTypeReasoning
}

// Normalize transforms one upstream/Bifrost stream event into zero or more normalized
// Responses SSE events with strictly increasing sequence_number values.
func (n *responsesStreamNormalizer) Normalize(ev *schemas.BifrostResponsesStreamResponse) []*schemas.BifrostResponsesStreamResponse {
	if ev == nil {
		return nil
	}

	switch ev.Type {
	case schemas.ResponsesStreamResponseTypeOutputItemAdded:
		if !isReasoningItem(ev.Item) {
			return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(cloneResponsesStreamEvent(ev))}
		}
		outIdx := 0
		if ev.OutputIndex != nil {
			outIdx = *ev.OutputIndex
		}
		itemID := ""
		if ev.Item.ID != nil {
			itemID = *ev.Item.ID
		}
		n.reasoning.reset(itemID, outIdx)

		copied := cloneResponsesStreamEvent(ev)
		copied.Item = normalizeResponsesReasoningItem(ev.Item, "in_progress", "")
		return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(copied)}

	case schemas.ResponsesStreamResponseTypeReasoningSummaryPartAdded:
		n.reasoning.emittedSummaryPart = true
		return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(cloneResponsesStreamEvent(ev))}

	case schemas.ResponsesStreamResponseTypeReasoningSummaryPartDone:
		n.reasoning.sawSummaryPartDone = true
		return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(cloneResponsesStreamEvent(ev))}

	case schemas.ResponsesStreamResponseTypeReasoningSummaryTextDone:
		n.reasoning.sawSummaryTextDone = true
		return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(cloneResponsesStreamEvent(ev))}

	case schemas.ResponsesStreamResponseTypeReasoningTextDelta:
		n.reasoning.emittedRawText = true
		if ev.Delta != nil {
			n.reasoning.textBuf.WriteString(*ev.Delta)
		}
		return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(cloneResponsesStreamEvent(ev))}

	case schemas.ResponsesStreamResponseTypeReasoningTextDone:
		n.reasoning.sawRawTextDone = true
		return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(cloneResponsesStreamEvent(ev))}

	case schemas.ResponsesStreamResponseTypeReasoningSummaryTextDelta:
		itemID, outIdx := n.resolveItemMeta(ev)
		if !n.reasoning.open {
			n.reasoning.reset(itemID, outIdx)
		}
		delta := ""
		if ev.Delta != nil {
			delta = *ev.Delta
		}

		// Native Responses upstreams carry SummaryIndex explicitly; only ensure part.added precedes it.
		if ev.SummaryIndex != nil {
			sumIdx := *ev.SummaryIndex
			n.reasoning.summaryBuf.WriteString(delta)
			var out []*schemas.BifrostResponsesStreamResponse
			if !n.reasoning.emittedSummaryPart {
				partAdded := &schemas.BifrostResponsesStreamResponse{
					Type:         schemas.ResponsesStreamResponseTypeReasoningSummaryPartAdded,
					ItemID:       strPtrIfNonEmpty(itemID),
					OutputIndex:  intPtr(outIdx),
					SummaryIndex: intPtr(sumIdx),
					Part:         summaryPartBlock(""),
					ExtraFields:  ev.ExtraFields,
				}
				out = append(out, n.nextEvent(partAdded))
				n.reasoning.emittedSummaryPart = true
			}
			copied := cloneResponsesStreamEvent(ev)
			out = append(out, n.nextEvent(copied))
			return out
		}

		// Synthesized Chat-to-Responses delta from Bifrost (SummaryIndex == nil):
		// Always emit compliant response.reasoning_text.delta (content_index: 0),
		// and also emit summary stream events unless summaryMode == "none".
		n.reasoning.textBuf.WriteString(delta)
		var out []*schemas.BifrostResponsesStreamResponse

		rawDelta := &schemas.BifrostResponsesStreamResponse{
			Type:         schemas.ResponsesStreamResponseTypeReasoningTextDelta,
			ItemID:       strPtrIfNonEmpty(itemID),
			OutputIndex:  intPtr(outIdx),
			ContentIndex: intPtr(0),
			Delta:        stringPtr(delta),
			ExtraFields:  ev.ExtraFields,
		}
		n.reasoning.emittedRawText = true
		out = append(out, n.nextEvent(rawDelta))

		if n.summaryMode != "none" {
			n.reasoning.summaryBuf.WriteString(delta)
			if !n.reasoning.emittedSummaryPart {
				partAdded := &schemas.BifrostResponsesStreamResponse{
					Type:         schemas.ResponsesStreamResponseTypeReasoningSummaryPartAdded,
					ItemID:       strPtrIfNonEmpty(itemID),
					OutputIndex:  intPtr(outIdx),
					SummaryIndex: intPtr(0),
					Part:         summaryPartBlock(""),
					ExtraFields:  ev.ExtraFields,
				}
				out = append(out, n.nextEvent(partAdded))
				n.reasoning.emittedSummaryPart = true
			}
			sumDelta := &schemas.BifrostResponsesStreamResponse{
				Type:         schemas.ResponsesStreamResponseTypeReasoningSummaryTextDelta,
				ItemID:       strPtrIfNonEmpty(itemID),
				OutputIndex:  intPtr(outIdx),
				SummaryIndex: intPtr(0),
				Delta:        stringPtr(delta),
				ExtraFields:  ev.ExtraFields,
			}
			out = append(out, n.nextEvent(sumDelta))
		}
		return out

	case schemas.ResponsesStreamResponseTypeOutputItemDone:
		if !isReasoningItem(ev.Item) {
			return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(cloneResponsesStreamEvent(ev))}
		}
		itemID, outIdx := n.resolveItemMeta(ev)
		if itemID == "" && ev.Item.ID != nil {
			itemID = *ev.Item.ID
		}
		if ev.OutputIndex != nil {
			outIdx = *ev.OutputIndex
		}

		var out []*schemas.BifrostResponsesStreamResponse
		if n.reasoning.emittedRawText && !n.reasoning.sawRawTextDone {
			fullText := n.reasoning.textBuf.String()
			out = append(out, n.nextEvent(&schemas.BifrostResponsesStreamResponse{
				Type:         schemas.ResponsesStreamResponseTypeReasoningTextDone,
				ItemID:       strPtrIfNonEmpty(itemID),
				OutputIndex:  intPtr(outIdx),
				ContentIndex: intPtr(0),
				Text:         stringPtr(fullText),
				ExtraFields:  ev.ExtraFields,
			}))
			n.reasoning.sawRawTextDone = true
		}

		summaryText := ""
		if n.reasoning.emittedSummaryPart {
			summaryText = n.reasoning.summaryBuf.String()
			if !n.reasoning.sawSummaryTextDone {
				out = append(out, n.nextEvent(&schemas.BifrostResponsesStreamResponse{
					Type:         schemas.ResponsesStreamResponseTypeReasoningSummaryTextDone,
					ItemID:       strPtrIfNonEmpty(itemID),
					OutputIndex:  intPtr(outIdx),
					SummaryIndex: intPtr(0),
					Text:         stringPtr(summaryText),
					ExtraFields:  ev.ExtraFields,
				}))
				n.reasoning.sawSummaryTextDone = true
			}
			if !n.reasoning.sawSummaryPartDone {
				out = append(out, n.nextEvent(&schemas.BifrostResponsesStreamResponse{
					Type:         schemas.ResponsesStreamResponseTypeReasoningSummaryPartDone,
					ItemID:       strPtrIfNonEmpty(itemID),
					OutputIndex:  intPtr(outIdx),
					SummaryIndex: intPtr(0),
					Part:         summaryPartBlock(summaryText),
					ExtraFields:  ev.ExtraFields,
				}))
				n.reasoning.sawSummaryPartDone = true
			}
		}

		copied := cloneResponsesStreamEvent(ev)
		copied.Item = normalizeResponsesReasoningItem(ev.Item, "completed", summaryText)
		out = append(out, n.nextEvent(copied))
		n.reasoning.open = false
		return out

	case schemas.ResponsesStreamResponseTypeCompleted,
		schemas.ResponsesStreamResponseTypeIncomplete,
		schemas.ResponsesStreamResponseTypeFailed:
		copied := cloneResponsesStreamEvent(ev)
		if copied.Response != nil {
			respCopy := *copied.Response
			if len(copied.Response.Output) > 0 {
				respCopy.Output = append([]schemas.ResponsesMessage(nil), copied.Response.Output...)
			}
			summaryText := ""
			if n.reasoning.emittedSummaryPart {
				summaryText = n.reasoning.summaryBuf.String()
			}
			normalizeResponsesOutputItemsWithSummary(&respCopy, summaryText)
			copied.Response = &respCopy
		}
		return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(copied)}

	default:
		return []*schemas.BifrostResponsesStreamResponse{n.nextEvent(cloneResponsesStreamEvent(ev))}
	}
}

func strPtrIfNonEmpty(s string) *string {
	if s == "" {
		return nil
	}
	v := s
	return &v
}
