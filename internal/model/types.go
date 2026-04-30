package model

// Kind is the PDF type.
type Kind string

const (
	KindShinki  Kind = "shinki"  // _kouriteika.pdf      新規認可
	KindHenkou  Kind = "henkou"  // _kouriteikahenkou.pdf 価格改定
	KindUnknown Kind = "unknown"
)

// PDFRef points to a single source PDF.
type PDFRef struct {
	Date       string `json:"date"`               // YYYY-MM-DD (認可日)
	Kind       Kind   `json:"kind"`               // shinki | henkou
	URL        string `json:"url"`                // primary 財務省 URL
	WaybackURL string `json:"waybackUrl,omitempty"` // fallback URL on Wayback Machine
	Filename   string `json:"filename"`           // basename
}

// Category is the top-level 製造たばこの区分.
type Category string

const (
	CategoryPipe      Category = "パイプたばこ"
	CategoryCigarette Category = "紙巻たばこ"
	CategoryCigar     Category = "葉巻たばこ"
	CategoryKizami    Category = "刻みたばこ"
	CategoryOther     Category = "その他"
)

// PipeType is our internal sub-classification for pipe tobacco.
type PipeType string

const (
	PipeTypeKiseru  PipeType = "kiseru"  // キセル / 通常パイプ
	PipeTypeShisha  PipeType = "shisha"  // 水タバコ
	PipeTypeUnknown PipeType = "unknown"
)

// ExtractedProduct is one row as returned by the LLM extractor for a single PDF.
type ExtractedProduct struct {
	Category     Category `json:"category"`
	Manufacturer string   `json:"manufacturer"`
	Name         string   `json:"name"`     // 名称 (full as printed)
	Variant      string   `json:"variant"`  // 製品の区分 / 形状 (e.g. "箱", "袋", "FK 20本")
	Grams        string   `json:"grams"`    // "50g", "250g" - kept as displayed
	PriceYen     int      `json:"priceYen"` // 小売定価
	Country      string   `json:"country"`  // 製造国(地)
}

// ExtractedPDF is the per-PDF JSON we cache under data/extracted/.
type ExtractedPDF struct {
	Date     string             `json:"date"`     // YYYY-MM-DD
	Kind     Kind               `json:"kind"`
	Filename string             `json:"filename"`
	URL      string             `json:"url"`      // URL actually fetched
	Products []ExtractedProduct `json:"products"`
}

// Product is the final DB record (extracted row + classification metadata).
type Product struct {
	Category     Category `json:"category"`
	Manufacturer string   `json:"manufacturer"`
	Name         string   `json:"name"`
	Variant      string   `json:"variant,omitempty"`
	Grams        string   `json:"grams"`
	PriceYen     int      `json:"priceYen"`
	Country      string   `json:"country,omitempty"`
	PipeType     PipeType `json:"pipeType,omitempty"`  // for パイプたばこ only
	UpdatedDate  string   `json:"updatedDate"`         // YYYY-MM-DD (出典PDF日)
	Source       Kind     `json:"source"`              // 出典PDF種別
	SourceURL    string   `json:"sourceUrl"`           // 出典PDF URL
}

// Key is the dedupe identifier across PDFs. Same SKU across multiple PDFs (e.g., a product
// initially appearing in shinki, later re-appearing in henkou with new price) collapses
// to a single record; the latest PDF wins.
func (p Product) Key() string {
	return string(p.Category) + "|" + p.Manufacturer + "|" + p.Name + "|" + p.Variant + "|" + p.Grams
}
