// Package extractor turns 財務省 認可たばこ PDFs into structured JSON with a
// multimodal LLM via OpenRouter. Models that read PDFs natively get the file
// as-is; image-only models get each page rendered to PNG.
package extractor

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/yeighta/flavor-authorization/internal/llm"
	"github.com/yeighta/flavor-authorization/internal/model"
	"github.com/yeighta/flavor-authorization/internal/pdf"
)

// DefaultModel won cmd/eval-extract (2026-10): 100% recall / 100% price on shisha rows,
// and the cheapest of the candidates. It reads PDFs natively. Override with EXTRACT_MODEL;
// image-only models (e.g. deepseek/deepseek-v4.1-flash) get pages rendered via pdftoppm.
const DefaultModel = "openai/gpt-6-luna"

// renderDPI balances legibility of half-width katakana against image tokens.
const renderDPI = 200

// Prompt sent with each PDF. Written in Japanese for fidelity to the source.
const promptText = `添付は日本の財務省が公開している「製造たばこ小売定価認可」の通知 PDF（または各ページの画像）です。
表に記載されている全ての製品行を JSON として抽出してください。

# 列の意味
- 製造たばこの区分: パイプたばこ / 紙巻たばこ / 葉巻たばこ / 刻みたばこ / その他 のいずれか。縦書きの場合あり。
  結合セルで複数行に 1 回だけ書かれている場合は、そのセルに含まれる全行に適用する。
- 名称: 製品名。多くの場合「ブランド名（メーカー名）+ 製品名」が連続している。
  例「Bang Bang Cappuccino」→ manufacturer="Bang Bang", name="Cappuccino"
  例「MAZAYA Babylon Mint」→ manufacturer="MAZAYA", name="Babylon Mint"
  階層レイアウトの場合、左カラム（結合セル）にメーカー名、右カラムに複数のフレーバー名が並ぶ。
- 製品の区分: 形状や容量の補足（例: "箱", "缶", "袋", "FK 20本", "170mm 1本"）。容量(g)はここに含まれることがある。
  容量と形状が上下 2 段に書かれている場合（"50.0g" の下に "箱"）は、容量を grams、形状を variant に入れる。
- 小売定価: 「1,750円」など。整数の円で返す。カンマ・"円"は除く。
  価格改定の通知（「現行小売定価」「変更小売定価」の 2 列がある表）では、必ず「変更小売定価」（新しい方）を返す。
- 製造国(地): 例「ﾖﾙﾀﾞﾝ」「日本」「ｱﾗﾌﾞ首長国連邦」。半角カナのまま。

# 出力ルール
- 文字は印字どおりに転記する。大文字・小文字、半角・全角、記号（' と ’ など）を書き換えない。
- 結合セルで価格・容量・区分を共有する複数製品は、それぞれ独立した行として展開する。
- g数は表示に応じて "50g" "50.0g" "250g" 等の文字列のまま保持。容量が無ければ空文字。
- 不明・読み取り不能なフィールドは空文字 (priceYen は 0)。
- ヘッダ行・注記・ページ番号は出力しない。
- 重複行は出力しない。
`

func schema() map[string]any {
	str := func(desc string) map[string]any {
		m := map[string]any{"type": "string"}
		if desc != "" {
			m["description"] = desc
		}
		return m
	}
	product := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"category":     str("パイプたばこ / 紙巻たばこ / 葉巻たばこ / 刻みたばこ / その他"),
			"manufacturer": str(""),
			"name":         str(""),
			"variant":      str(`形状。例 "箱" "缶" "袋"。無ければ空文字。`),
			"grams":        str(`表示通りの容量。例 "50g" "50.0g" "250g"。無ければ空文字。`),
			"priceYen":     map[string]any{"type": "integer", "description": "小売定価（改定通知では変更後）。円単位の整数。"},
			"country":      str(""),
		},
		"required":             []string{"category", "manufacturer", "name", "variant", "grams", "priceYen", "country"},
		"additionalProperties": false,
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"products": map[string]any{"type": "array", "items": product},
		},
		"required":             []string{"products"},
		"additionalProperties": false,
	}
}

// Client extracts product rows from PDFs.
type Client struct {
	llm       *llm.Client
	nativePDF bool
}

// NewClient picks the model from EXTRACT_MODEL (default DefaultModel) and asks
// OpenRouter whether it reads PDFs natively.
func NewClient(ctx context.Context) (*Client, error) {
	name := os.Getenv("EXTRACT_MODEL")
	if name == "" {
		name = DefaultModel
	}
	return NewClientForModel(ctx, name)
}

// NewClientForModel is NewClient with an explicit model (used by eval-extract).
func NewClientForModel(ctx context.Context, name string) (*Client, error) {
	c, err := llm.NewClient(name)
	if err != nil {
		return nil, err
	}
	info, err := c.LookupModel(ctx)
	if err != nil {
		return nil, err
	}
	return &Client{llm: c, nativePDF: info.AcceptsFile()}, nil
}

// Model reports the LLM model in use and how PDFs are passed (for logging).
func (c *Client) Model() string {
	if c.nativePDF {
		return c.llm.Model + " (native PDF)"
	}
	return fmt.Sprintf("%s (PNG @%ddpi)", c.llm.Model, renderDPI)
}

// Cost is the cumulative USD cost reported by OpenRouter.
func (c *Client) Cost() float64 { return c.llm.Cost }

// ExtractPDF sends the PDF (or its rendered pages) to the LLM and returns the parsed product list.
func (c *Client) ExtractPDF(ctx context.Context, pdfPath string) ([]model.ExtractedProduct, error) {
	parts := []llm.Part{}
	if c.nativePDF {
		b, err := os.ReadFile(pdfPath)
		if err != nil {
			return nil, err
		}
		parts = append(parts, llm.PDFPart(filepath.Base(pdfPath), b))
	} else {
		pages, err := pdf.RenderPNG(ctx, pdfPath, renderDPI)
		if err != nil {
			return nil, err
		}
		for _, p := range pages {
			parts = append(parts, llm.PNGPart(p))
		}
	}
	parts = append(parts, llm.TextPart("上記の表から全ての製品行を抽出してください。"))

	const maxAttempts = 2
	var lastErr error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		raw, err := c.llm.Complete(ctx, llm.Request{
			Messages: []llm.Message{
				{Role: "system", Content: promptText},
				{Role: "user", Content: parts},
			},
			Schema:     schema(),
			SchemaName: "products",
			MaxTokens:  65536,
			NativePDF:  c.nativePDF,
		})
		if err != nil {
			return nil, err
		}
		var parsed struct {
			Products []model.ExtractedProduct `json:"products"`
		}
		if err := json.Unmarshal([]byte(llm.ExtractJSON(raw)), &parsed); err != nil {
			// Malformed JSON occasionally happens even with structured output; one retry is cheap.
			lastErr = fmt.Errorf("parse extractor json: %w (raw head: %.200s)", err, raw)
			continue
		}
		return parsed.Products, nil
	}
	return nil, lastErr
}
