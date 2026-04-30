// Package classifier uses Gemini to classify pipe-tobacco manufacturers
// as kiseru (キセル/通常パイプ) or shisha (水タバコ).
package classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"google.golang.org/genai"

	"github.com/yeighta/flavor-authorization/internal/model"
)

const DefaultModel = "gemini-3-flash-preview"

// Classification carries one manufacturer's verdict.
type Classification struct {
	Manufacturer string         `json:"manufacturer"`
	Type         model.PipeType `json:"type"`               // kiseru | shisha | unknown
	Confidence   string         `json:"confidence"`         // high | medium | low
	Reason       string         `json:"reason,omitempty"`
	// Locked entries are preserved across `classify --refresh`; set this
	// to protect manual judgments that disagree with the LLM verdict.
	Locked bool `json:"locked,omitempty"`
}

// Map is the on-disk format keyed by manufacturer.
type Map struct {
	Entries map[string]Classification `json:"entries"`
}

// Sample is the input we send the LLM for one manufacturer.
type Sample struct {
	Manufacturer string   `json:"manufacturer"`
	Countries    []string `json:"countries"`
	ProductNames []string `json:"productNames"`
}

const promptText = `あなたは日本のたばこ市場（特にパイプたばこ）に詳しい専門家です。
入力は財務省で「パイプたばこ」として認可されているメーカーのリストです。
必要に応じて Google 検索を使い、各メーカーの公式サイト・販売店・SNS等から
事実を確認した上で、以下の3種類に分類してください。

# 分類カテゴリ
- "kiseru": キセル（刻みたばこ）または通常のパイプ用たばこ。
  代表例: JT「小粋」「桃山」, Mac Baren, Peterson, Dunhill, Captain Black, Davidoff(パイプ),
         Borkum Riff, Lane Limited, Stanwell, Kohlhase & Kopp 等。
  製造国: ドイツ・デンマーク・アイルランド・米国・日本 等が多い。
- "shisha": 水タバコ（フーカ／シーシャ）用たばこ。
  代表例: Al Fakher, Starbuzz, MAZAYA, JiBiAR, REVOSHI, Bang Bang, Adalya, Nakhla,
         Fumari, Tangiers, Social Smoke, Trifecta, Haze, Azure, Fantasia, DARKSIDE, MARY JANE 等。
  製造国: UAE・ヨルダン・トルコ・エジプト・米国 等が多い。
- "unknown": Web検索を使っても確信を持って分類できない場合のみ。

# 判定の根拠
1. 製品名・製造国・ブランド名から仮説を立てる
2. Web検索で公式情報を確認（ブランド公式サイト・販売ページ等）
3. 検索結果から得た事実をreasonに含める

- 製品名にフレーバー名（フルーツ・スイーツ・カクテル・ミント等）が並ぶ → 大半がシーシャ
- 製品名が "Mixture", "Cavendish", "Burley", "Aromatic" など伝統的キセル/パイプ用語 → kiseru
- 50g/250g/1kgパッケージで甘いフレーバー多数 → shisha
- 紙巻きや葉巻専門の名前は出てこない（前提: 全件パイプたばこ認可済）

# 出力フォーマット
**JSON のみを返してください。マークダウン・説明文・コードフェンスは含めない**。
形式は以下の通り（Web検索で得た情報を reason に簡潔に含める）：

{
  "classifications": [
    {"manufacturer": "...", "type": "shisha|kiseru|unknown", "confidence": "high|medium|low", "reason": "..."}
  ]
}
`

// Client wraps the Gen AI client.
type Client struct {
	c     *genai.Client
	Model string
}

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

// Classify sends a batch of manufacturer samples to Gemini and returns classifications.
func (c *Client) Classify(ctx context.Context, samples []Sample) ([]Classification, error) {
	body, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		return nil, err
	}
	contents := []*genai.Content{{
		Role: "user",
		Parts: []*genai.Part{
			{Text: promptText},
			{Text: "# 入力\n```json\n" + string(body) + "\n```"},
		},
	}}
	cfg := &genai.GenerateContentConfig{
		// Google Search grounding is mutually exclusive with ResponseSchema/ResponseMIMEType=json.
		// We instruct the model to emit raw JSON in the prompt and parse defensively.
		Tools:          []*genai.Tool{{GoogleSearch: &genai.GoogleSearch{}}},
		Temperature:    genai.Ptr[float32](0),
		ThinkingConfig: &genai.ThinkingConfig{ThinkingBudget: genai.Ptr[int32](0)},
	}
	res, err := c.c.Models.GenerateContent(ctx, c.Model, contents, cfg)
	if err != nil {
		return nil, fmt.Errorf("gemini classify: %w", err)
	}
	raw := extractJSON(res.Text())
	var parsed struct {
		Classifications []Classification `json:"classifications"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse classify json: %w (raw head: %s)", err, head(raw, 200))
	}
	return parsed.Classifications, nil
}

// extractJSON unwraps the LLM's JSON response from common decorations
// (markdown code fences, leading/trailing commentary). Falls back to the
// largest brace-balanced substring if simpler unwrapping fails.
func extractJSON(s string) string {
	s = strings.TrimSpace(s)
	// Strip ```json ... ``` or ``` ... ```
	if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```json")
		s = strings.TrimPrefix(s, "```")
		s = strings.TrimSpace(s)
		if i := strings.LastIndex(s, "```"); i >= 0 {
			s = s[:i]
		}
		s = strings.TrimSpace(s)
	}
	// If still wrapped in commentary, find the outermost {...}.
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
