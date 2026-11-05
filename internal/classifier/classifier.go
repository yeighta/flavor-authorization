// Package classifier uses an LLM (with web search) to classify pipe-tobacco
// manufacturers as kiseru (キセル/通常パイプ) or shisha (水タバコ).
package classifier

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"golang.org/x/text/unicode/norm"

	"github.com/yeighta/flavor-authorization/internal/llm"
	"github.com/yeighta/flavor-authorization/internal/model"
)

// Classification carries one manufacturer's verdict.
type Classification struct {
	Manufacturer string         `json:"manufacturer"`
	Type         model.PipeType `json:"type"`       // kiseru | shisha | unknown
	Confidence   string         `json:"confidence"` // high | medium | low
	Reason       string         `json:"reason,omitempty"`
	// BrandFound reports whether the search results describe this brand itself,
	// as opposed to the verdict being inferred from neighbouring brands.
	BrandFound bool `json:"brandFound"`
	// Sources are the pages the web search actually returned for this verdict.
	Sources []string `json:"sources,omitempty"`
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

const promptText = `あなたは日本のたばこ市場に詳しい専門家です。
入力は財務省で「パイプたばこ」として小売定価が認可された1つのメーカー（ブランド）です。
Web 検索結果に出てくる販売店・公式サイト・SNS の記述を根拠に、次のどちらかに分類してください。

- "shisha": 水タバコ（フーカ／シーシャ）用のたばこ。
  例: Al Fakher, Starbuzz, MAZAYA, Adalya, Nakhla, Tangiers, DARKSIDE, DOGMA
- "kiseru": シーシャ以外。キセル用の刻み、パイプ用たばこ、
  オーラルたばこ（ニコチンパウチ・スヌース等、「15個缶」「20個缶」のような個数表記）など。
  例: 小粋, Peterson, Mac Baren, Captain Black, VELO
- "unknown": 検索結果からどちらとも判断できない場合。推測で決めないこと。

# 注意
- フレーバー名（フルーツ・ミント・スイーツ等）は判断材料になりません。
  パイプたばこやオーラルたばこにも同じようなフレーバー名が付きます。
- 容量・包装（100g〜1kg の缶や袋はシーシャに多い、30〜50g パウチはパイプにも多い、個数表記はオーラル）は参考にしてよいですが、
  決め手は検索結果に「シーシャ」「水タバコ」「hookah」「パイプ」「スヌース」などの記述があるかです。
- 財務省の PDF は分類の根拠になりません。
- brandFound: 検索結果に、このブランド（またはその製品）そのものについての記述があれば true。
  同じ PDF に載っている別ブランドや、シーシャ一般の説明しか見つからない場合は false にし、type は "unknown" にすること。
- reason には、根拠にした検索結果の記述を簡潔に書いてください。検索結果にない情報は書かないこと。
`

func schema() map[string]any {
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"brandFound": map[string]any{"type": "boolean"},
			"type":       map[string]any{"type": "string", "enum": []string{"shisha", "kiseru", "unknown"}},
			"confidence": map[string]any{"type": "string", "enum": []string{"high", "medium", "low"}},
			"reason":     map[string]any{"type": "string"},
		},
		"required":             []string{"brandFound", "type", "confidence", "reason"},
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

// Cost is the USD spent so far.
func (c *Client) Cost() float64 { return c.llm.Cost }

// Classify asks about one manufacturer. One request per manufacturer matters:
// the web plugin searches once per request, so a batch gets one search that
// matches none of its members and the model falls back to guessing.
func (c *Client) Classify(ctx context.Context, s Sample) (Classification, error) {
	body, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return Classification{}, err
	}
	res, cites, err := c.llm.CompleteCited(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: "system", Content: promptText},
			{Role: "user", Content: question(s) + "\n\n```json\n" + string(body) + "\n```"},
		},
		Schema:     schema(),
		SchemaName: "classification",
		MaxTokens:  2048,
		WebSearch:  true,
	})
	if err != nil {
		return Classification{}, fmt.Errorf("classify %s: %w", s.Manufacturer, err)
	}
	var cl Classification
	if err := json.Unmarshal([]byte(llm.ExtractJSON(res)), &cl); err != nil {
		return Classification{}, fmt.Errorf("parse classify json for %s: %w (raw head: %.200s)", s.Manufacturer, err, res)
	}
	cl.Manufacturer = s.Manufacturer
	cl.Sources = evidence(cites)
	return ground(cl), nil
}

// question leads the user message because the web plugin builds its search
// query from it.
func question(s Sample) string {
	// NFKC turns half-width katakana from the PDFs (ｷｭｰﾋﾞｯｸ) into the
	// full-width spelling shops actually use, which search matches far better.
	var names []string
	for _, n := range s.ProductNames {
		if len(names) == 3 {
			break
		}
		names = append(names, norm.NFKC.String(n))
	}
	q := fmt.Sprintf("たばこブランド「%s」", norm.NFKC.String(s.Manufacturer))
	if len(s.Countries) > 0 {
		q += "（" + strings.Join(s.Countries, "・") + "製）"
	}
	if len(names) > 0 {
		q += "の「" + strings.Join(names, "」「") + "」"
	}
	return q + "は、シーシャ（水タバコ）用ですか、それともパイプ・キセル・オーラルたばこなどですか。"
}

// evidence drops sources that cannot support a verdict: the 財務省 PDF the
// manufacturer came from says nothing about how the product is used.
func evidence(cites []string) []string {
	var out []string
	for _, c := range cites {
		if u, err := url.Parse(c); err == nil && strings.HasSuffix(u.Hostname(), "mof.go.jp") {
			continue
		}
		out = append(out, c)
	}
	return out
}

// ground demotes a verdict that no search result about the brand backs up to
// unknown, so a guess never publishes (or hides) a brand on its own.
func ground(cl Classification) Classification {
	if (len(cl.Sources) > 0 && cl.BrandFound) || cl.Type == model.PipeTypeUnknown {
		return cl
	}
	why := "検索結果なし"
	if len(cl.Sources) > 0 {
		why = "ブランドの記述なし"
	}
	cl.Reason = fmt.Sprintf("%s（モデルの推測は %s）: %s", why, cl.Type, cl.Reason)
	cl.Type = model.PipeTypeUnknown
	cl.Confidence = "low"
	return cl
}
