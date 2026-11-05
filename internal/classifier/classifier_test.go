package classifier

import (
	"strings"
	"testing"

	"github.com/yeighta/flavor-authorization/internal/model"
)

func TestQuestionLeadsWithBrandAndProducts(t *testing.T) {
	q := question(Sample{
		Manufacturer: "ｷｭｰﾋﾞｯｸ",
		Countries:    []string{"パキスタン"},
		ProductNames: []string{"A", "B", "C", "D"},
	})
	if !strings.HasPrefix(q, "たばこブランド「キュービック」（パキスタン製）の「A」「B」「C」は") {
		t.Errorf("question = %q", q)
	}
}

func TestEvidenceDropsMOF(t *testing.T) {
	got := evidence([]string{
		"https://www.mof.go.jp/policy/tab_salt/topics/20251010_kouriteika.pdf",
		"https://www.tabako.co.jp/item/x/",
	})
	if len(got) != 1 || got[0] != "https://www.tabako.co.jp/item/x/" {
		t.Errorf("evidence = %v", got)
	}
}

func TestGroundDemotesUnsourcedVerdicts(t *testing.T) {
	cl := ground(Classification{Type: model.PipeTypeShisha, Confidence: "high", Reason: "guess"})
	if cl.Type != model.PipeTypeUnknown || cl.Confidence != "low" || !strings.Contains(cl.Reason, "shisha") {
		t.Errorf("unsourced verdict kept: %+v", cl)
	}
	offTopic := Classification{Type: model.PipeTypeShisha, Sources: []string{"https://example.com"}}
	if got := ground(offTopic); got.Type != model.PipeTypeUnknown {
		t.Errorf("verdict without the brand in the results kept: %+v", got)
	}
	sourced := Classification{Type: model.PipeTypeShisha, Confidence: "high", BrandFound: true, Sources: []string{"https://example.com"}}
	if got := ground(sourced); got.Type != model.PipeTypeShisha {
		t.Errorf("sourced verdict demoted: %+v", got)
	}
}
