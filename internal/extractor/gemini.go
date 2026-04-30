// Package extractor turns 財務省 認可たばこ PDFs into structured JSON
// using a multimodal LLM (Gemini).
package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"google.golang.org/genai"

	"github.com/yeighta/flavor-authorization/internal/model"
)

const DefaultModel = "gemini-3-flash-preview"

// Prompt sent with each PDF. Written in Japanese for fidelity to the source.
const promptText = `この PDF は日本の財務省が公開している「製造たばこ小売定価認可」の通知です。
表に記載されている全ての製品行を JSON 配列として抽出してください。

# 列の意味
- 製造たばこの区分: パイプたばこ / 紙巻たばこ / 葉巻たばこ / 刻みたばこ / その他 のいずれか。縦書きの場合あり。
- 名称: 製品名。多くの場合「ブランド名（メーカー名）+ 製品名」が連続している。
  例「Bang Bang Cappuccino」→ manufacturer="Bang Bang", name="Cappuccino"
  例「MAZAYA Babylon Mint」→ manufacturer="MAZAYA", name="Babylon Mint"
  階層レイアウトの場合、左カラムにメーカー名、右カラムに複数のフレーバー名が並ぶ。
- 製品の区分: 形状や容量の補足（例: "箱", "袋", "FK 20本", "170mm 1本"）。容量(g)はここに含まれることがある。
- 小売定価: 「1,750円」など。整数の円で返す。カンマ・"円"は除く。
- 製造国(地): 例「ﾖﾙﾀﾞﾝ」「日本」「ｱﾗﾌﾞ首長国連邦」。半角カナのまま。

# 出力ルール
- 価格が同じグループに属する複数製品は、それぞれ独立した行として展開する。
- g数は表示に応じて "50g" "50.0g" "250g" 等の文字列のまま保持。容量が無ければ空文字。
- 不明・読み取り不能なフィールドは空文字 (priceYen は 0)。
- 縦書きの「パイプたばこ」「紙巻たばこ」等の区分ラベルも漏らさず読み取る。
- 重複行は出力しない。
`

// jsonSchema enforces the response structure.
func jsonSchema() *genai.Schema {
	productSchema := &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"category":     {Type: genai.TypeString, Description: "パイプたばこ / 紙巻たばこ / 葉巻たばこ / 刻みたばこ / その他"},
			"manufacturer": {Type: genai.TypeString},
			"name":         {Type: genai.TypeString},
			"variant":      {Type: genai.TypeString},
			"grams":        {Type: genai.TypeString, Description: `表示通りの容量。例 "50g" "50.0g" "250g"。無ければ空文字。`},
			"priceYen":     {Type: genai.TypeInteger, Description: "小売定価。円単位の整数。"},
			"country":      {Type: genai.TypeString},
		},
		Required: []string{"category", "manufacturer", "name", "grams", "priceYen", "country"},
	}
	return &genai.Schema{
		Type: genai.TypeObject,
		Properties: map[string]*genai.Schema{
			"products": {Type: genai.TypeArray, Items: productSchema},
		},
		Required: []string{"products"},
	}
}

// Client wraps the Gen AI client.
type Client struct {
	c     *genai.Client
	Model string
}

// NewClient returns a Gemini client. Reads GEMINI_API_KEY by default (Gen AI SDK convention).
func NewClient(ctx context.Context) (*Client, error) {
	apiKey := os.Getenv("GEMINI_API_KEY")
	if apiKey == "" {
		return nil, fmt.Errorf("GEMINI_API_KEY env var is not set")
	}
	c, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
	if err != nil {
		return nil, err
	}
	return &Client{c: c, Model: DefaultModel}, nil
}

// ExtractPDF sends the PDF bytes inline to Gemini and returns the parsed product list.
// Transient failures (504/503/429/network) are retried with exponential backoff.
func (c *Client) ExtractPDF(ctx context.Context, pdfBytes []byte) ([]model.ExtractedProduct, error) {
	contents := []*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{
			{Text: promptText},
			{InlineData: &genai.Blob{
				MIMEType: "application/pdf",
				Data:     pdfBytes,
			}},
		},
	}}
	cfg := &genai.GenerateContentConfig{
		ResponseMIMEType: "application/json",
		ResponseSchema:   jsonSchema(),
		Temperature:      genai.Ptr[float32](0),
		// PDF table transcription is a perception task; reasoning isn't needed.
		// Thinking is on by default for gemini-3-flash and silently consumes the
		// MaxOutputTokens budget, causing premature MAX_TOKENS truncation.
		ThinkingConfig:  &genai.ThinkingConfig{ThinkingBudget: genai.Ptr[int32](0)},
		MaxOutputTokens: 65536,
	}

	const maxAttempts = 3
	backoffs := []time.Duration{5 * time.Second, 20 * time.Second}

	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		res, err := c.c.Models.GenerateContent(ctx, c.Model, contents, cfg)
		if err != nil {
			lastErr = err
			if !isTransient(err) || attempt == maxAttempts {
				return nil, fmt.Errorf("gemini generate: %w", err)
			}
			wait := backoffs[attempt-1]
			log.Printf("    gemini transient error (attempt %d/%d): %v — retrying in %s",
				attempt, maxAttempts, err, wait)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		raw := res.Text()
		finish := finishReason(res)
		// Treat anything other than a clean STOP as transient (MAX_TOKENS, SAFETY, RECITATION, etc.).
		bad := raw == "" || (finish != "" && finish != "STOP" && finish != "MODEL_LENGTH")
		if bad {
			lastErr = fmt.Errorf("bad response (finish=%q, raw=%dB)", finish, len(raw))
			if attempt == maxAttempts {
				return nil, lastErr
			}
			wait := backoffs[attempt-1]
			log.Printf("    gemini bad response (attempt %d/%d): %v — retrying in %s", attempt, maxAttempts, lastErr, wait)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}

		var parsed struct {
			Products []model.ExtractedProduct `json:"products"`
		}
		if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
			// Truncated JSON usually means an upstream issue with the response stream.
			// Retry rather than fail outright, since the same prompt sometimes succeeds.
			lastErr = fmt.Errorf("parse gemini json: %w", err)
			if attempt == maxAttempts {
				return nil, fmt.Errorf("%w (raw head: %s)", lastErr, head(raw, 200))
			}
			wait := backoffs[attempt-1]
			log.Printf("    gemini json parse failed (attempt %d/%d, %dB): retrying in %s", attempt, maxAttempts, len(raw), wait)
			select {
			case <-time.After(wait):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
			continue
		}
		return parsed.Products, nil
	}
	return nil, lastErr
}

// finishReason returns the first candidate's finish reason as a string.
// Empty string means no candidate or no reason was reported.
func finishReason(res *genai.GenerateContentResponse) string {
	if res == nil || len(res.Candidates) == 0 {
		return ""
	}
	return string(res.Candidates[0].FinishReason)
}

func head(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// isTransient classifies errors that should be retried. Includes Gemini
// 5xx server errors, 429 rate limits, and DEADLINE_EXCEEDED responses.
func isTransient(err error) bool {
	s := err.Error()
	for _, marker := range []string{
		"DEADLINE_EXCEEDED",
		"UNAVAILABLE",
		"RESOURCE_EXHAUSTED",
		"INTERNAL",
		"Error 429",
		"Error 500",
		"Error 502",
		"Error 503",
		"Error 504",
		"context deadline exceeded",
	} {
		if strings.Contains(s, marker) {
			return true
		}
	}
	return false
}
