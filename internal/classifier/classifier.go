// Package classifier uses an LLM (with web search) to classify pipe-tobacco
// manufacturers as kiseru (キセル/通常パイプ) or shisha (水タバコ).
package classifier

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yeighta/flavor-authorization/internal/llm"
	"github.com/yeighta/flavor-authorization/internal/model"
)

// Classification carries one manufacturer's verdict.
type Classification struct {
	Manufacturer string         `json:"manufacturer"`
	Type         model.PipeType `json:"type"`       // kiseru | shisha | unknown
	Confidence   string         `json:"confidence"` // high | medium | low
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
必要に応じて Web 検索結果を参照し、各メーカーの公式サイト・販売店・SNS等から
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

func schema() map[string]any {
	item := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"manufacturer": map[string]any{"type": "string"},
			"type":         map[string]any{"type": "string", "enum": []string{"shisha", "kiseru", "unknown"}},
			"confidence":   map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
			"reason":       map[string]any{"type": "string"},
		},
		"required":             []string{"manufacturer", "type", "confidence", "reason"},
		"additionalProperties": false,
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"classifications": map[string]any{"type": "array", "items": item},
		},
		"required":             []string{"classifications"},
		"additionalProperties": false,
	}
}

// Client wraps the LLM client.
type Client struct {
	llm *llm.Client
}

func NewClient() (*Client, error) {
	c, err := llm.NewClient("")
	if err != nil {
		return nil, err
	}
	return &Client{llm: c}, nil
}

// Classify sends a batch of manufacturer samples to the LLM and returns classifications.
func (c *Client) Classify(ctx context.Context, samples []Sample) ([]Classification, error) {
	body, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		return nil, err
	}
	res, err := c.llm.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: promptText},
			{Role: "user", Content: "# 入力\n```json\n" + string(body) + "\n```"},
		},
		Schema:     schema(),
		SchemaName: "classifications",
		MaxTokens:  16384,
		// OpenRouter's web plugin stands in for Gemini's Google Search grounding.
		WebSearch: true,
	})
	if err != nil {
		return nil, fmt.Errorf("classify: %w", err)
	}
	raw := llm.ExtractJSON(res)
	var parsed struct {
		Classifications []Classification `json:"classifications"`
	}
	if err := json.Unmarshal([]byte(raw), &parsed); err != nil {
		return nil, fmt.Errorf("parse classify json: %w (raw head: %.200s)", err, raw)
	}
	return parsed.Classifications, nil
}
