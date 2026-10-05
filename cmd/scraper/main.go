// scraper downloads PDFs listed in data/pdf-urls.json, runs each through a
// multimodal LLM (OpenRouter),
// caches the per-PDF result under data/extracted/, then merges everything into
// data/products.json. Idempotent: PDFs already in the cache are skipped.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
	"golang.org/x/text/unicode/norm"
	"golang.org/x/text/width"

	"github.com/yeighta/flavor-authorization/internal/classifier"
	"github.com/yeighta/flavor-authorization/internal/extractor"
	"github.com/yeighta/flavor-authorization/internal/model"
	"github.com/yeighta/flavor-authorization/internal/normalizer"
	"github.com/yeighta/flavor-authorization/internal/pdf"
)

func main() {
	urlsPath := flag.String("urls", "data/pdf-urls.json", "input PDF URL list")
	cacheDir := flag.String("cache", "data/extracted", "per-PDF JSON cache directory")
	productsPath := flag.String("out", "data/products.json", "merged products DB output")
	aliasesPath := flag.String("aliases", "data/manufacturer-aliases.json", "manufacturer alias map (optional)")
	manufacturersPath := flag.String("manufacturers", "data/manufacturers.json", "manufacturer classification map (optional, used to drop kiseru)")
	limit := flag.Int("limit", 0, "max PDFs to process (0 = all). Useful for incremental runs.")
	rps := flag.Float64("rps", 0.2, "max LLM requests per second")
	skipExtract := flag.Bool("skip-extract", false, "skip extraction; only re-merge cache → products.json")
	flag.Parse()

	_ = godotenv.Load() // best-effort load of .env (no error if absent)

	if err := os.MkdirAll(*cacheDir, 0755); err != nil {
		log.Fatal(err)
	}

	refs, err := loadURLs(*urlsPath)
	if err != nil {
		log.Fatalf("load urls: %v", err)
	}
	log.Printf("loaded %d PDF refs", len(refs))

	if !*skipExtract {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Hour)
		defer cancel()

		client, err := extractor.NewClient(ctx)
		if err != nil {
			log.Fatalf("llm client: %v", err)
		}
		log.Printf("extractor model: %s", client.Model())
		fetcher := pdf.NewFetcher()

		interval := time.Duration(float64(time.Second) / *rps)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()

		processed, skipped, failed := 0, 0, 0
		for i, ref := range refs {
			if *limit > 0 && processed >= *limit {
				log.Printf("reached --limit %d, stopping", *limit)
				break
			}
			cachePath := filepath.Join(*cacheDir, cacheName(ref))
			if _, err := os.Stat(cachePath); err == nil {
				skipped++
				continue
			}
			<-ticker.C
			t0 := time.Now()
			log.Printf("[%d/%d] %s %s", i+1, len(refs), ref.Date, ref.Filename)
			// 3 attempts × LLM deadline + retry backoffs ≈ up to 6 min worst case.
			perPDFCtx, perPDFCancel := context.WithTimeout(ctx, 6*time.Minute)
			err := processOne(perPDFCtx, client, fetcher, ref, cachePath)
			perPDFCancel()
			if err != nil {
				log.Printf("  ERR (%.1fs): %v", time.Since(t0).Seconds(), err)
				failed++
				continue
			}
			log.Printf("  OK (%.1fs)", time.Since(t0).Seconds())
			processed++
		}
		log.Printf("done. processed=%d skipped=%d failed=%d", processed, skipped, failed)
		log.Printf("llm cost: $%.4f", client.Cost())
	}

	if err := mergeProducts(*cacheDir, *productsPath, *aliasesPath, *manufacturersPath); err != nil {
		log.Fatalf("merge: %v", err)
	}
}

func loadURLs(path string) ([]model.PDFRef, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var refs []model.PDFRef
	if err := json.NewDecoder(f).Decode(&refs); err != nil {
		return nil, err
	}
	// Process oldest first so the merged DB's "latest wins" rule mirrors chronology.
	sort.Slice(refs, func(i, j int) bool { return refs[i].Date < refs[j].Date })
	return refs, nil
}

func cacheName(ref model.PDFRef) string {
	base := strings.TrimSuffix(ref.Filename, filepath.Ext(ref.Filename))
	return base + ".json"
}

func processOne(ctx context.Context, client *extractor.Client, fetcher *pdf.Fetcher, ref model.PDFRef, cachePath string) error {
	tmp, err := os.MkdirTemp("", "pdf-*")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	pdfPath := filepath.Join(tmp, ref.Filename)

	usedURL, err := fetcher.Fetch(ctx, ref, pdfPath)
	if err != nil {
		return fmt.Errorf("fetch: %w", err)
	}

	products, err := client.ExtractPDF(ctx, pdfPath)
	if err != nil {
		return err
	}
	out := model.ExtractedPDF{
		Date:     ref.Date,
		Kind:     ref.Kind,
		Filename: ref.Filename,
		URL:      usedURL,
		Products: products,
	}
	return writeJSON(cachePath, out)
}

func writeJSON(path string, v any) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

// mergeProducts walks the cache dir, applies the "latest PDF wins" merge rule, and writes products.json.
// When a later PDF (typically a 価格改定) hits an existing Key(), only price / date / source are
// updated; the display fields from the earlier row are kept so the product doesn't flip names or
// lose its variant just because the henkou PDF printed them differently.
// If aliasesPath exists, manufacturer names are canonicalized before deduplication.
// If manufacturersPath exists, パイプたばこ rows whose manufacturer is classified as kiseru
// are dropped entirely (the site is shisha-focused).
func mergeProducts(cacheDir, outPath, aliasesPath, manufacturersPath string) error {
	aliases := loadAliases(aliasesPath)
	if len(aliases) > 0 {
		log.Printf("loaded %d manufacturer aliases from %s", len(aliases), aliasesPath)
	}
	classifications := loadClassifications(manufacturersPath)
	if len(classifications) > 0 {
		log.Printf("loaded %d manufacturer classifications from %s", len(classifications), manufacturersPath)
	}

	entries, err := os.ReadDir(cacheDir)
	if err != nil {
		return err
	}
	type cached struct {
		PDF model.ExtractedPDF
	}
	var all []cached
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
			continue
		}
		f, err := os.Open(filepath.Join(cacheDir, e.Name()))
		if err != nil {
			return err
		}
		var c model.ExtractedPDF
		err = json.NewDecoder(f).Decode(&c)
		f.Close()
		if err != nil {
			return fmt.Errorf("decode %s: %w", e.Name(), err)
		}
		all = append(all, cached{PDF: c})
	}
	// Process older first; later writes override (latest price wins).
	// Tie-break on Filename so when multiple PDFs share a date, the surviving
	// SourceURL is deterministic across runs (otherwise products.json flips
	// between runs and creates phantom diffs).
	sort.Slice(all, func(i, j int) bool {
		if all[i].PDF.Date != all[j].PDF.Date {
			return all[i].PDF.Date < all[j].PDF.Date
		}
		return all[i].PDF.Filename < all[j].PDF.Filename
	})

	merged := map[string]model.Product{}
	dropped := 0
	for _, c := range all {
		for _, ep := range c.PDF.Products {
			manufacturer := strings.TrimSpace(ep.Manufacturer)
			if canonical, ok := aliases[manufacturer]; ok {
				manufacturer = canonical
			}
			cat := normalizeCategory(ep.Category)
			// The site is shisha-focused: ship only パイプたばこ rows, and within those
			// drop kiseru-classified manufacturers. Everything else (葉巻, 紙巻, その他, …)
			// stays in data/extracted/ as raw history but is excluded from the public DB.
			if cat != model.CategoryPipe {
				dropped++
				continue
			}
			// Empty manufacturer = LLM extraction failure. Confirmed offline that these
			// are not shisha rows, so drop them rather than show an unusable bucket.
			if manufacturer == "" {
				dropped++
				continue
			}
			// Drop both kiseru and unknown classifications. Unknown manufacturers
			// have empirically all turned out to be non-shisha; if a real shisha
			// brand ever lands in unknown, it can be promoted via manual edit
			// of data/manufacturers.json (set type=shisha) and a re-merge.
			if cls, ok := classifications[manufacturer]; ok {
				if cls.Type == model.PipeTypeKiseru || cls.Type == model.PipeTypeUnknown {
					dropped++
					continue
				}
			} else {
				// No classification at all = unclassified manufacturer; drop too.
				dropped++
				continue
			}
			p := model.Product{
				Category:     cat,
				Manufacturer: manufacturer,
				Name:         normalizeProductName(ep.Name),
				Variant:      normalizeVariant(ep.Variant),
				Grams:        strings.TrimSpace(ep.Grams),
				PriceYen:     ep.PriceYen,
				Country:      normalizeCountry(ep.Country),
				UpdatedDate:  c.PDF.Date,
				Source:       c.PDF.Kind,
				SourceURL:    c.PDF.URL,
			}
			if prev, ok := merged[p.Key()]; ok {
				p = updatePrice(prev, p)
			} else {
				p.History = []model.PricePoint{pricePoint(p)}
			}
			merged[p.Key()] = p
		}
	}
	if dropped > 0 {
		log.Printf("dropped %d non-shisha rows (non-pipe categories or kiseru-classified)", dropped)
	}

	out := make([]model.Product, 0, len(merged))
	for _, p := range merged {
		out = append(out, p)
	}
	unifyDisplayNames(out)
	// Sort by full Key() so output ordering is deterministic across runs.
	// Previously only (Category, Manufacturer, Name) was used, leaving Variant
	// and Grams as non-deterministic tie-breakers.
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return writeJSON(outPath, out)
}

// normalizeCountry trims whitespace and widens half-width katakana / ASCII
// digits & symbols to their full-width forms. Source PDFs use 半角カナ
// (ﾄﾙｺ, ｱﾗﾌﾞ首長国連邦); the public DB normalizes to full-width (トルコ,
// アラブ首長国連邦) so display and filter values are stable.
func normalizeCountry(s string) string {
	s = norm.NFC.String(width.Widen.String(strings.TrimSpace(s)))
	// Stray underscores and ideographic spaces come from table cell wrapping,
	// e.g. "アメリカ合衆＿国", "ロシア　モルドバ".
	s = strings.ReplaceAll(s, "＿", "")
	return strings.Join(strings.FieldsFunc(s, func(r rune) bool { return r == ' ' || r == 0x3000 }), "・")
}

// normalizeProductName cleans the text (see normalizeText) and applies English
// title case (see titleCaseEnglishWords).
func normalizeProductName(s string) string {
	return titleCaseEnglishWords(spacedPossessive.ReplaceAllString(normalizeText(s), "$1'$2"))
}

// spacedPossessive repairs "Barista ' s Choice" (the PDF kerns the apostrophe apart).
var spacedPossessive = regexp.MustCompile(`([A-Za-z])\s*'\s+([sS])\b`)

// normalizeText widens half-width katakana, narrows full-width ASCII back so we
// don't end up with full-width Latin characters, unifies curly apostrophes and
// collapses whitespace.
func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = strings.NewReplacer("’", "'", "‘", "'").Replace(s)
	// Widen splits ﾌﾞ into フ + U+3099; NFC recomposes it to ブ.
	s = norm.NFC.String(width.Widen.String(s))
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		// All fullwidth ASCII printables U+FF01–U+FF5E → narrow back (offset −0xFEE0).
		// Covers letters, digits, punctuation (' " - . , : ; ! ? etc.).
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		// Ideographic space and runs of spaces (left by line wrapping) → one ASCII space.
		case r == 0x3000 || r == ' ':
			if !strings.HasSuffix(b.String(), " ") {
				b.WriteRune(' ')
			}
		default:
			b.WriteRune(r)
		}
	}
	return strings.TrimSpace(b.String())
}

var leadingGrams = regexp.MustCompile(`^\d+(\.\d+)?\s*g\s*`)

// normalizeVariant cleans 製品の区分. Some PDFs print the pack size in this column
// too ("100.0g 箱"); grams already has its own field, so it is dropped here.
func normalizeVariant(s string) string {
	return strings.TrimSpace(leadingGrams.ReplaceAllString(normalizeText(s), ""))
}

// titleCaseEnglishWords normalizes the casing of ASCII words to English title
// case, because the same flavor is printed as "ALOHA NIGHTS", "Aloha nights" and
// "Aloha Nights" across PDFs. Short function words stay lowercase ("Cherry with
// Mint"), intentional mixed case is kept ("McLaren"), and short all-caps tokens
// are treated as acronyms ("JT", "USA") unless the whole name is in capitals.
func titleCaseEnglishWords(s string) string {
	allCaps := strings.ToUpper(s) == s
	out := make([]byte, 0, len(s))
	first := true
	for i := 0; i < len(s); {
		j := i
		for j < len(s) && isWordByte(s[j]) {
			j++
		}
		if j == i {
			out = append(out, s[i])
			i++
			continue
		}
		out = append(out, caseWord(s[i:j], first, allCaps)...)
		first = false
		i = j
	}
	return string(out)
}

var smallWords = map[string]bool{
	"a": true, "an": true, "and": true, "at": true, "by": true, "de": true, "del": true, "du": true,
	"for": true, "in": true, "la": true, "le": true, "of": true, "on": true, "or": true, "the": true,
	"to": true, "with": true,
}

// shortWords are 2–3 letter words that PDFs print in capitals but that are not
// acronyms, so "Elite Edition ICE Mint" becomes "Elite Edition Ice Mint".
var shortWords = map[string]bool{
	"ace": true, "air": true, "big": true, "box": true, "da": true, "day": true, "fig": true, "gin": true,
	"gum": true, "hot": true, "ice": true, "joy": true, "key": true, "mix": true, "new": true, "nut": true,
	"old": true, "one": true, "pie": true, "red": true, "rum": true, "sex": true, "six": true, "sky": true,
	"sun": true, "tea": true, "ten": true, "top": true, "two": true, "zen": true,
}

func caseWord(w string, first, allCaps bool) string {
	// Case the stem of possessives and contractions on its own: "VALENTINE's" → "Valentine's".
	if i := strings.IndexByte(w, '\''); i > 0 {
		return caseWord(w[:i], first, allCaps) + strings.ToLower(w[i:])
	}
	lower := strings.ToLower(w)
	if !hasLetter(w) {
		return w
	}
	if !first && smallWords[lower] {
		return lower
	}
	switch {
	case w == strings.ToUpper(w):
		// ALL CAPS: title-case real words, keep short acronyms unless everything is shouting.
		if len(w) >= 4 || allCaps || shortWords[lower] {
			return capitalize(lower)
		}
		return w
	case w == lower:
		return capitalize(lower)
	case w == capitalize(lower):
		return w
	default:
		// Deliberate mixed case such as "McLaren" or "iPhone".
		return w
	}
}

func capitalize(w string) string {
	for k := 0; k < len(w); k++ {
		if w[k] >= 'a' && w[k] <= 'z' {
			return w[:k] + string(w[k]-32) + w[k+1:]
		}
		if w[k] >= 'A' && w[k] <= 'Z' {
			return w
		}
	}
	return w
}

func hasLetter(w string) bool {
	for k := 0; k < len(w); k++ {
		if (w[k] >= 'a' && w[k] <= 'z') || (w[k] >= 'A' && w[k] <= 'Z') {
			return true
		}
	}
	return false
}

func isWordByte(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '\'' || c >= 0x80
}

// loadClassifications reads the optional manufacturer classification map. Returns empty if missing.
func loadClassifications(path string) map[string]classifier.Classification {
	f, err := os.Open(path)
	if err != nil {
		return map[string]classifier.Classification{}
	}
	defer f.Close()
	var m classifier.Map
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		log.Printf("warn: parse classifications %s: %v", path, err)
		return map[string]classifier.Classification{}
	}
	if m.Entries == nil {
		return map[string]classifier.Classification{}
	}
	return m.Entries
}

// loadAliases reads the optional manufacturer alias map. Returns empty map if missing.
func loadAliases(path string) map[string]string {
	f, err := os.Open(path)
	if err != nil {
		return map[string]string{}
	}
	defer f.Close()
	var m normalizer.AliasMap
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		log.Printf("warn: parse aliases %s: %v", path, err)
		return map[string]string{}
	}
	if m.Aliases == nil {
		return map[string]string{}
	}
	return m.Aliases
}

// normalizeCategory canonicalizes the LLM's free-text category labels
// to the official 製造たばこの区分 set. Tolerates OCR/transcription drift like
// "パイたばこ" or "パイサたばこ" when the LLM misread the rotated header.
func normalizeCategory(c model.Category) model.Category {
	s := strings.TrimSpace(string(c))
	switch s {
	case "パイプたばこ", "パイたばこ", "パイサたばこ", "パイブたばこ":
		return model.CategoryPipe
	case "紙巻たばこ", "紙巻きたばこ":
		return model.CategoryCigarette
	case "葉巻たばこ":
		return model.CategoryCigar
	case "刻みたばこ", "きざみたばこ":
		return model.CategoryKizami
	}
	return model.Category(s)
}

// updatePrice applies a later sighting of the same product onto the existing record:
// price and provenance move forward, descriptive fields stay unless they were empty.
func updatePrice(prev, next model.Product) model.Product {
	out := prev
	// priceYen 0 means the extractor couldn't read it; don't clobber a known price.
	if next.PriceYen != 0 {
		if next.PriceYen != prev.PriceYen {
			out.History = append(append([]model.PricePoint(nil), prev.History...), pricePoint(next))
		}
		out.PriceYen = next.PriceYen
	}
	out.UpdatedDate = next.UpdatedDate
	out.Source = next.Source
	out.SourceURL = next.SourceURL
	if out.Variant == "" {
		out.Variant = next.Variant
	}
	if out.Country == "" {
		out.Country = next.Country
	}
	if out.Grams == "" {
		out.Grams = next.Grams
	}
	return out
}

func pricePoint(p model.Product) model.PricePoint {
	return model.PricePoint{Date: p.UpdatedDate, PriceYen: p.PriceYen, Source: p.Source, SourceURL: p.SourceURL}
}

// unifyDisplayNames gives every pack size of the same flavor one spelling.
// Sizes are separate products (their Key includes grams), so "Water melon with
// Mint" 50g and "Watermelon with Mint" 250g would otherwise both be shown.
// The spelling from the earliest authorization wins.
func unifyDisplayNames(ps []model.Product) {
	type pick struct {
		name, date string
	}
	best := map[string]pick{}
	group := func(p model.Product) string {
		return model.Product{Category: p.Category, Manufacturer: p.Manufacturer, Name: p.Name}.Key()
	}
	for _, p := range ps {
		date := p.UpdatedDate
		if len(p.History) > 0 {
			date = p.History[0].Date
		}
		g := group(p)
		if b, ok := best[g]; !ok || date < b.date || (date == b.date && p.Name < b.name) {
			best[g] = pick{p.Name, date}
		}
	}
	for i := range ps {
		ps[i].Name = best[group(ps[i])].name
	}
}
