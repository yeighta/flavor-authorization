// eval-extract compares candidate extraction models against the cached
// extractions in data/extracted/ (produced by Gemini 3 Flash) on a fixed,
// deliberately diverse sample of PDFs: merged cells, wrapped names, large
// multi-page henkou tables, and 2024– PDFs whose text layer is broken.
//
// The cache is not perfect ground truth, so read low scores together with the
// per-PDF diff files written to -out.
//
//	go run ./cmd/eval-extract -models deepseek/deepseek-v4.1-flash,google/gemini-3.1-flash-lite
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/joho/godotenv"
	"golang.org/x/text/unicode/norm"

	"github.com/yeighta/flavor-authorization/internal/extractor"
	"github.com/yeighta/flavor-authorization/internal/model"
	"github.com/yeighta/flavor-authorization/internal/pdf"
)

var defaultSample = []string{
	"20180926_kouriteika_2.pdf",     // 12p, old layout
	"20190314_kouriteika.pdf",       // names wrapped across lines
	"20200914_kouriteikahenkou.pdf", // 18p henkou
	"20241016_kouriteikahenkou.pdf", // broken text layer
	"20250318_kouriteikahenkou.pdf", // broken text layer
	"20250828_kouriteika.pdf",       // broken text layer
	"20260825_kouriteikahenkou.pdf", // small
	"20261002_kouriteikahenkou.pdf", // merged manufacturer / price cells
}

type score struct {
	ref, got, matched, exactName, price, variant int
	cost                                         float64
	dur                                          time.Duration
	failed                                       int
}

func main() {
	models := flag.String("models", extractor.DefaultModel, "comma-separated OpenRouter model IDs")
	files := flag.String("files", strings.Join(defaultSample, ","), "comma-separated PDF filenames from data/pdf-urls.json")
	cacheDir := flag.String("cache", "data/extracted", "reference extraction cache")
	outDir := flag.String("out", "eval-out", "where per-model outputs and diffs are written")
	flag.Parse()
	_ = godotenv.Load()

	var all []model.PDFRef
	b, err := os.ReadFile("data/pdf-urls.json")
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(b, &all); err != nil {
		log.Fatal(err)
	}
	refs := map[string]model.PDFRef{}
	for _, r := range all {
		refs[r.Filename] = r
	}

	ctx := context.Background()
	tmp, err := os.MkdirTemp("", "eval-*")
	if err != nil {
		log.Fatal(err)
	}
	defer os.RemoveAll(tmp)

	// Download once, share across models.
	fetcher := pdf.NewFetcher()
	var sample []string
	for _, f := range strings.Split(*files, ",") {
		ref, ok := refs[f]
		if !ok {
			log.Fatalf("%s not in pdf-urls.json", f)
		}
		if _, err := fetcher.Fetch(ctx, ref, filepath.Join(tmp, f)); err != nil {
			log.Fatalf("fetch %s: %v", f, err)
		}
		sample = append(sample, f)
	}

	modelList := strings.Split(*models, ",")
	results := map[string]*score{}
	for _, m := range modelList {
		c, err := extractor.NewClientForModel(ctx, m)
		if err != nil {
			log.Fatalf("%s: %v", m, err)
		}
		log.Printf("== %s", c.Model())
		s := &score{}
		results[m] = s
		modelDir := filepath.Join(*outDir, strings.ReplaceAll(m, "/", "_"))
		if err := os.MkdirAll(modelDir, 0755); err != nil {
			log.Fatal(err)
		}
		for _, f := range sample {
			base := strings.TrimSuffix(f, ".pdf")
			ref := pipeRows(loadCache(filepath.Join(*cacheDir, base+".json")))
			t0 := time.Now()
			got, err := c.ExtractPDF(ctx, filepath.Join(tmp, f))
			s.dur += time.Since(t0)
			if err != nil {
				log.Printf("  %s: ERR %v", f, err)
				s.failed++
				s.ref += len(ref)
				continue
			}
			got = pipeRows(got)
			matched, report := compare(ref, got, s)
			log.Printf("  %s: ref=%d got=%d matched=%d (%.1fs)", f, len(ref), len(got), matched, time.Since(t0).Seconds())
			writeJSON(filepath.Join(modelDir, base+".json"), got)
			_ = os.WriteFile(filepath.Join(modelDir, base+".diff.txt"), []byte(report), 0644)
		}
		s.cost = c.Cost()
	}

	fmt.Printf("\n%-36s %7s %7s %9s %8s %8s %8s %6s\n", "model", "recall", "prec.", "nameExact", "price", "variant", "cost$", "time")
	for _, m := range modelList {
		s := results[m]
		fmt.Printf("%-36s %6.1f%% %6.1f%% %8.1f%% %7.1f%% %7.1f%% %8.4f %5.0fs",
			m, pct(s.matched, s.ref), pct(s.matched, s.got), pct(s.exactName, s.matched),
			pct(s.price, s.matched), pct(s.variant, s.matched), s.cost, s.dur.Seconds())
		if s.failed > 0 {
			fmt.Printf("  (%d PDFs failed)", s.failed)
		}
		fmt.Println()
	}
	fmt.Printf("\nper-PDF outputs and diffs: %s/\n", *outDir)
}

// compare matches rows on folded manufacturer+name and grams, so a model that splits
// "Azure hookah tobacco Gold Line X" differently from the reference still matches.
func compare(ref, got []model.ExtractedProduct, s *score) (int, string) {
	key := func(p model.ExtractedProduct) string {
		return fold(p.Manufacturer+p.Name) + "|" + fold(p.Grams)
	}
	refBy := map[string][]model.ExtractedProduct{}
	for _, p := range ref {
		refBy[key(p)] = append(refBy[key(p)], p)
	}
	var lines []string
	matched := 0
	for _, p := range got {
		k := key(p)
		cands := refBy[k]
		if len(cands) == 0 {
			lines = append(lines, "+ extra   "+row(p))
			continue
		}
		r := cands[0]
		refBy[k] = cands[1:]
		matched++
		if nfkc(r.Manufacturer+" "+r.Name) == nfkc(p.Manufacturer+" "+p.Name) {
			s.exactName++
		} else {
			lines = append(lines, "~ name    "+row(r)+"  =>  "+row(p))
		}
		if r.PriceYen == p.PriceYen {
			s.price++
		} else {
			lines = append(lines, "~ price   "+row(r)+"  =>  "+row(p))
		}
		if fold(r.Variant) == fold(p.Variant) {
			s.variant++
		}
	}
	for _, rest := range refBy {
		for _, p := range rest {
			lines = append(lines, "- missing "+row(p))
		}
	}
	sort.Strings(lines)
	s.ref += len(ref)
	s.got += len(got)
	s.matched += matched
	return matched, strings.Join(lines, "\n") + "\n"
}

func row(p model.ExtractedProduct) string {
	return fmt.Sprintf("[%s | %s | %s | %s | ¥%d]", p.Manufacturer, p.Name, p.Variant, p.Grams, p.PriceYen)
}

func pipeRows(ps []model.ExtractedProduct) []model.ExtractedProduct {
	var out []model.ExtractedProduct
	for _, p := range ps {
		if strings.Contains(string(p.Category), "パイ") {
			out = append(out, p)
		}
	}
	return out
}

// nfkc compares "as printed" modulo half/full-width and whitespace runs, which
// merge-time normalization erases anyway. Case and ’ vs ' still count as different.
func nfkc(s string) string {
	return strings.Join(strings.Fields(norm.NFKC.String(s)), " ")
}

func fold(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(norm.NFKC.String(s)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func pct(a, b int) float64 {
	if b == 0 {
		return 0
	}
	return 100 * float64(a) / float64(b)
}

func loadCache(path string) []model.ExtractedProduct {
	var e model.ExtractedPDF
	b, err := os.ReadFile(path)
	if err != nil {
		log.Fatal(err)
	}
	if err := json.Unmarshal(b, &e); err != nil {
		log.Fatal(err)
	}
	return e.Products
}

func writeJSON(path string, v any) {
	b, _ := json.MarshalIndent(v, "", "  ")
	_ = os.WriteFile(path, b, 0644)
}
