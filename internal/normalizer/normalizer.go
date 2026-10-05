// Package normalizer collapses spelling variants of the same brand into a
// canonical name, using an LLM for fuzzy matching.
package normalizer

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/yeighta/flavor-authorization/internal/llm"
)

// Cluster is a group of manufacturer names that the LLM judges to be
// variants of the same brand.
type Cluster struct {
	Names []string `json:"names"`
}

// Sample is what we feed the LLM for one manufacturer.
type Sample struct {
	Name         string   `json:"name"`
	Countries    []string `json:"countries"`
	ProductNames []string `json:"productNames"`
	Count        int      `json:"count"` // total product rows under this name
}

const promptText = `あなたは日本のたばこ市場（特にパイプたばこ）に詳しい専門家です。
入力は財務省PDFから自動抽出したメーカー名のリストです。OCR・LLM抽出の揺れにより、
同じブランドが複数の表記で並んでいることがあります。

# タスク
表記揺れ・別名・部分名のみを「同一ブランド」としてクラスタ化してください。
クラスタ化の判断基準：
- 大文字小文字違い (例: "AL FAKHER" / "Al Fakher" / "AL Fakher")
- 空白・記号の違い (例: "Al Fakher" / "Al-Fakher" / "AlFakher")
- 接尾辞の有無 (例: "Trifecta" / "Trifecta Tobacco" / "Trifecta Hookah")
- 製造国とサンプル製品が一致しており、明らかに同じブランドであること

異なるブランドは絶対に統合しないこと。判断に迷う場合は別クラスタにする。
- "Al Fakher" と "Al Waha" は別ブランド (どちらもヨルダン製水タバコだが別企業)
- "Eternal Smoke" と "Smoke" は別ブランド

# 出力
clusters 配列に、2件以上の名前を含むクラスタのみを返す（単独メーカーは出力しない）。
各クラスタ内には全ての別名表記を含める。
`

func responseSchema() map[string]any {
	cluster := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"names": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		},
		"required":             []string{"names"},
		"additionalProperties": false,
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"clusters": map[string]any{"type": "array", "items": cluster},
		},
		"required":             []string{"clusters"},
		"additionalProperties": false,
	}
}

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

// Cluster sends the samples to the LLM and returns groups of variant names.
func (c *Client) Cluster(ctx context.Context, samples []Sample) ([]Cluster, error) {
	body, err := json.MarshalIndent(samples, "", "  ")
	if err != nil {
		return nil, err
	}
	res, err := c.llm.Complete(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: promptText},
			{Role: "user", Content: "# 入力\n```json\n" + string(body) + "\n```"},
		},
		Schema:     responseSchema(),
		SchemaName: "clusters",
		MaxTokens:  32768,
	})
	if err != nil {
		return nil, fmt.Errorf("cluster: %w", err)
	}
	var parsed struct {
		Clusters []Cluster `json:"clusters"`
	}
	if err := json.Unmarshal([]byte(llm.ExtractJSON(res)), &parsed); err != nil {
		return nil, fmt.Errorf("parse cluster json: %w", err)
	}
	return parsed.Clusters, nil
}

// AliasMap is the on-disk mapping from raw → canonical manufacturer name.
type AliasMap struct {
	// Aliases maps every variant (including the canonical itself) to its canonical form.
	Aliases map[string]string `json:"aliases"`
}

// PickCanonical returns the canonical name for a cluster:
//  1. The variant with the most product rows wins.
//  2. Tie-break: shortest name.
//  3. Tie-break: alphabetical.
func PickCanonical(cluster []string, counts map[string]int) string {
	best := cluster[0]
	for _, n := range cluster[1:] {
		switch {
		case counts[n] > counts[best]:
			best = n
		case counts[n] == counts[best] && len(n) < len(best):
			best = n
		case counts[n] == counts[best] && len(n) == len(best) && n < best:
			best = n
		}
	}
	return best
}
