// Package llm is a minimal OpenRouter (OpenAI-compatible) chat completions client
// shared by the extractor, classifier and normalizer.
package llm

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	Endpoint     = "https://openrouter.ai/api/v1/chat/completions"
	DefaultModel = "deepseek/deepseek-v4-flash"
)

// Client talks to OpenRouter.
type Client struct {
	HTTP   *http.Client
	APIKey string
	Model  string
	// Cost accumulates the USD cost OpenRouter reports for each successful call.
	Cost float64
}

// NewClient reads OPENROUTER_API_KEY (required). model picks the model; when empty,
// OPENROUTER_MODEL and then DefaultModel are used.
func NewClient(model string) (*Client, error) {
	key := os.Getenv("OPENROUTER_API_KEY")
	if key == "" {
		return nil, fmt.Errorf("OPENROUTER_API_KEY env var is not set")
	}
	if model == "" {
		model = os.Getenv("OPENROUTER_MODEL")
	}
	if model == "" {
		model = DefaultModel
	}
	return &Client{
		HTTP:   &http.Client{Timeout: 5 * time.Minute},
		APIKey: key,
		Model:  model,
	}, nil
}

// Message content is either a plain string or a []Part for multimodal input.
type Message struct {
	Role    string `json:"role"`
	Content any    `json:"content"`
}

// Part is one element of a multimodal message.
type Part map[string]any

func TextPart(s string) Part { return Part{"type": "text", "text": s} }

// PNGPart embeds a PNG image as a data URL.
func PNGPart(png []byte) Part {
	return Part{"type": "image_url", "image_url": map[string]any{
		"url": "data:image/png;base64," + base64.StdEncoding.EncodeToString(png),
	}}
}

// PDFPart embeds a PDF as a file part. Only use it with models that read PDFs
// natively (input_modalities contains "file") together with Request.NativePDF.
func PDFPart(filename string, pdf []byte) Part {
	return Part{"type": "file", "file": map[string]any{
		"filename":  filename,
		"file_data": "data:application/pdf;base64," + base64.StdEncoding.EncodeToString(pdf),
	}}
}

// Request holds the knobs we actually use. Schema, when set, is sent as a strict
// json_schema response_format; WebSearch enables OpenRouter's web plugin.
type Request struct {
	Messages   []Message
	Schema     map[string]any
	SchemaName string
	MaxTokens  int
	WebSearch  bool
	// NativePDF forces OpenRouter to hand PDFs to the model as-is instead of
	// falling back to its text-layer parser (which garbles many 財務省 PDFs).
	NativePDF bool
}

type chatRequest struct {
	Model          string           `json:"model"`
	Messages       []Message        `json:"messages"`
	Temperature    float64          `json:"temperature"`
	MaxTokens      int              `json:"max_tokens,omitempty"`
	ResponseFormat map[string]any   `json:"response_format,omitempty"`
	Reasoning      map[string]any   `json:"reasoning,omitempty"`
	Plugins        []map[string]any `json:"plugins,omitempty"`
	Usage          map[string]any   `json:"usage,omitempty"`
}

type chatResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
		FinishReason string `json:"finish_reason"`
	} `json:"choices"`
	Usage struct {
		Cost float64 `json:"cost"`
	} `json:"usage"`
	Error *struct {
		Code    any    `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// Complete sends the request and returns the assistant's text. Transient failures
// (429/5xx/network/truncated output) are retried with backoff.
func (c *Client) Complete(ctx context.Context, r Request) (string, error) {
	body := chatRequest{
		Model:       c.Model,
		Messages:    r.Messages,
		Temperature: 0,
		MaxTokens:   r.MaxTokens,
		// Table transcription / classification doesn't benefit from reasoning, and
		// reasoning tokens would eat into MaxTokens.
		Reasoning: map[string]any{"enabled": false},
		Usage:     map[string]any{"include": true},
	}
	if r.Schema != nil {
		name := r.SchemaName
		if name == "" {
			name = "response"
		}
		body.ResponseFormat = map[string]any{
			"type": "json_schema",
			"json_schema": map[string]any{
				"name":   name,
				"strict": true,
				"schema": r.Schema,
			},
		}
	}
	if r.WebSearch {
		body.Plugins = append(body.Plugins, map[string]any{"id": "web", "max_results": 5})
	}
	if r.NativePDF {
		body.Plugins = append(body.Plugins, map[string]any{"id": "file-parser", "pdf": map[string]any{"engine": "native"}})
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return "", err
	}

	const maxAttempts = 3
	backoffs := []time.Duration{5 * time.Second, 20 * time.Second}
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		text, retry, err := c.do(ctx, payload)
		if err == nil {
			return text, nil
		}
		lastErr = err
		if !retry || attempt == maxAttempts {
			break
		}
		wait := backoffs[attempt-1]
		log.Printf("    openrouter error (attempt %d/%d): %v — retrying in %s", attempt, maxAttempts, err, wait)
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return "", ctx.Err()
		}
	}
	return "", lastErr
}

func (c *Client) do(ctx context.Context, payload []byte) (text string, retry bool, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint, bytes.NewReader(payload))
	if err != nil {
		return "", false, err
	}
	req.Header.Set("Authorization", "Bearer "+c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("HTTP-Referer", "https://github.com/yeighta/flavor-authorization")
	req.Header.Set("X-Title", "flavor-authorization")

	res, err := c.HTTP.Do(req)
	if err != nil {
		return "", true, err
	}
	defer res.Body.Close()
	raw, err := io.ReadAll(res.Body)
	if err != nil {
		return "", true, err
	}
	if res.StatusCode != http.StatusOK {
		retry := res.StatusCode == 429 || res.StatusCode >= 500
		return "", retry, fmt.Errorf("HTTP %d: %s", res.StatusCode, head(string(raw), 300))
	}
	var parsed chatResponse
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", true, fmt.Errorf("decode response: %w", err)
	}
	if parsed.Error != nil {
		return "", true, fmt.Errorf("upstream error %v: %s", parsed.Error.Code, parsed.Error.Message)
	}
	if len(parsed.Choices) == 0 {
		return "", true, fmt.Errorf("no choices in response")
	}
	ch := parsed.Choices[0]
	if ch.Message.Content == "" || (ch.FinishReason != "" && ch.FinishReason != "stop") {
		return "", true, fmt.Errorf("bad response (finish=%q, content=%dB)", ch.FinishReason, len(ch.Message.Content))
	}
	c.Cost += parsed.Usage.Cost
	return ch.Message.Content, false, nil
}

// ModelInfo is the subset of OpenRouter's model catalog we care about.
type ModelInfo struct {
	ID           string `json:"id"`
	Architecture struct {
		InputModalities []string `json:"input_modalities"`
	} `json:"architecture"`
}

// AcceptsFile reports whether the model reads PDFs natively.
func (m ModelInfo) AcceptsFile() bool {
	for _, x := range m.Architecture.InputModalities {
		if x == "file" {
			return true
		}
	}
	return false
}

// LookupModel fetches the model's catalog entry from OpenRouter (public endpoint).
func (c *Client) LookupModel(ctx context.Context) (ModelInfo, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://openrouter.ai/api/v1/models", nil)
	if err != nil {
		return ModelInfo{}, err
	}
	res, err := c.HTTP.Do(req)
	if err != nil {
		return ModelInfo{}, err
	}
	defer res.Body.Close()
	var list struct {
		Data []ModelInfo `json:"data"`
	}
	if err := json.NewDecoder(res.Body).Decode(&list); err != nil {
		return ModelInfo{}, fmt.Errorf("decode model list: %w", err)
	}
	for _, m := range list.Data {
		if m.ID == c.Model {
			return m, nil
		}
	}
	return ModelInfo{}, fmt.Errorf("model %q not found on OpenRouter", c.Model)
}

// ExtractJSON unwraps a JSON object from common decorations (markdown code
// fences, leading/trailing commentary).
func ExtractJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSpace(s)
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	if !strings.HasPrefix(s, "{") {
		first := strings.Index(s, "{")
		last := strings.LastIndex(s, "}")
		if first >= 0 && last > first {
			s = s[first : last+1]
		}
	}
	return s
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
