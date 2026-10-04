// Package llm is the seam between the agent loop and the model provider.
//
// The loop depends on the small Client interface below, not on the SDK
// client, so tests and evals can point it at a scripted fake and the loop
// stays the same code that runs in production.
package llm

import (
	"context"
	"fmt"

	"github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
)

// Request is one call to the model.
type Request struct {
	Model     string
	System    string
	Messages  []anthropic.MessageParam
	Tools     []anthropic.ToolUnionParam
	MaxTokens int64
	// NoTools tells the model to answer in text only. The agent sets it on
	// the last permitted round so a turn always ends with an answer.
	NoTools bool
}

// Client generates one assistant message. onText, if not nil, is called with
// each piece of text as it arrives. On error the returned message, if not
// nil, is whatever was received before the failure, for accounting only.
type Client interface {
	Generate(ctx context.Context, req Request, onText func(string)) (*anthropic.Message, error)
}

// Anthropic calls the Messages API with streaming.
type Anthropic struct {
	client anthropic.Client
}

// NewAnthropic builds a client. With no options it reads ANTHROPIC_API_KEY
// from the environment, as the tutorial did.
func NewAnthropic(opts ...option.RequestOption) *Anthropic {
	return &Anthropic{client: anthropic.NewClient(opts...)}
}

func (a *Anthropic) Generate(ctx context.Context, req Request, onText func(string)) (*anthropic.Message, error) {
	params := anthropic.MessageNewParams{
		Model:     anthropic.Model(req.Model),
		MaxTokens: req.MaxTokens,
		Messages:  req.Messages,
		Tools:     req.Tools,
	}
	if req.System != "" {
		params.System = []anthropic.TextBlockParam{{Text: req.System}}
	}
	if req.NoTools && len(req.Tools) > 0 {
		params.ToolChoice = anthropic.ToolChoiceUnionParam{OfNone: &anthropic.ToolChoiceNoneParam{}}
	}

	stream := a.client.Messages.NewStreaming(ctx, params)
	defer stream.Close()

	var msg anthropic.Message
	for stream.Next() {
		event := stream.Current()
		if err := msg.Accumulate(event); err != nil {
			return nil, fmt.Errorf("accumulate stream: %w", err)
		}
		if onText != nil && event.Type == "content_block_delta" && event.Delta.Type == "text_delta" {
			onText(event.Delta.Text)
		}
	}
	if err := stream.Err(); err != nil {
		// Return what arrived before the failure. The caller must not use it
		// as a reply, but its token counts were billed and have to be
		// accounted for.
		return &msg, err
	}
	return &msg, nil
}

// Price is the cost of a model in US dollars per million tokens.
type Price struct{ Input, Output float64 }

// Prices are list prices from the Claude pricing page, checked 2026-10-04.
// They are used for the demo's spending cap and the cost shown in the UI,
// so they err on the side of the documented figure rather than any discount.
var Prices = map[string]Price{
	"claude-haiku-4-5":  {1, 5},
	"claude-sonnet-5":   {2, 10},
	"claude-sonnet-5-5": {2, 10},
	"claude-opus-5":     {5, 25},
	"claude-opus-5-5":   {4, 20},
	"claude-fable-5-1":  {10, 50},
}

// WebSearchPricePer1K is the price of the server-side web search tool.
const WebSearchPricePer1K = 10.0

// fallbackPrice is used for models missing from the table. It is the most
// expensive entry, so an unknown model can only make the budget stricter.
var fallbackPrice = Price{10, 50}

// Cost estimates what a response cost.
func Cost(model string, inputTokens, outputTokens, webSearches int64) float64 {
	p, ok := Prices[model]
	if !ok {
		p = fallbackPrice
	}
	return float64(inputTokens)/1e6*p.Input +
		float64(outputTokens)/1e6*p.Output +
		float64(webSearches)/1000*WebSearchPricePer1K
}
