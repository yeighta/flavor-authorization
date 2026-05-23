// scraper downloads PDFs listed in data/pdf-urls.json, runs each through Gemini,
// caches the per-PDF result under data/extracted/, then merges everything into
// data/products.json. Idempotent: PDFs already in the cache are skipped.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"
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
	rps := flag.Float64("rps", 0.2, "max gemini requests per second (Flash free tier is 15/min ≈ 0.25/s)")
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
			log.Fatalf("gemini client: %v", err)
		}
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
			// 3 attempts × ~120s gemini deadline + retry backoffs ≈ up to 6 min worst case.
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

	bytes, err := os.ReadFile(pdfPath)
	if err != nil {
		return err
	}
	products, err := client.ExtractPDF(ctx, bytes)
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

// mergeProducts walks the cache dir, applies "latest PDF wins" merge rule, and writes products.json.
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
				Variant:      strings.TrimSpace(ep.Variant),
				Grams:        strings.TrimSpace(ep.Grams),
				PriceYen:     ep.PriceYen,
				Country:      normalizeCountry(ep.Country),
				UpdatedDate:  c.PDF.Date,
				Source:       c.PDF.Kind,
				SourceURL:    c.PDF.URL,
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
	// Sort by full Key() so output ordering is deterministic across runs.
	// Previously only (Category, Manufacturer, Name) was used, leaving Variant
	// and Grams as non-deterministic tie-breakers.
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return writeJSON(outPath, out)
}

// silence io import lint (kept for future streaming use)
var _ = io.Discard

// normalizeCountry trims whitespace and widens half-width katakana / ASCII
// digits & symbols to their full-width forms. Source PDFs use 半角カナ
// (ﾄﾙｺ, ｱﾗﾌﾞ首長国連邦); the public DB normalizes to full-width (トルコ,
// アラブ首長国連邦) so display and filter values are stable.
func normalizeCountry(s string) string {
	return strings.TrimSpace(width.Widen.String(s))
}

// normalizeProductName widens half-width katakana to full-width and applies
// Title Case to all-uppercase ASCII words of length ≥ 4 (so "DOUBLE APPLE"
// → "Double Apple" but short tokens like "JT", "USA" are preserved).
// ASCII letters that get incidentally widened by width.Widen are narrowed
// back so we don't end up with full-width Latin characters.
func normalizeProductName(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return s
	}
	s = width.Widen.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		switch {
		// All fullwidth ASCII printables U+FF01–U+FF5E → narrow back (offset −0xFEE0).
		// Covers letters, digits, punctuation (' " - . , : ; ! ? etc.).
		case r >= 0xFF01 && r <= 0xFF5E:
			b.WriteRune(r - 0xFEE0)
		// Ideographic space → ASCII space.
		case r == 0x3000:
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	return titleCaseEnglishWords(b.String())
}

// titleCaseEnglishWords converts pure-ASCII all-uppercase words (length ≥ 4)
// to Title Case. Mixed-case words and short uppercase tokens (likely acronyms)
// are left untouched.
func titleCaseEnglishWords(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); {
		// Find the next ASCII word boundary; non-ASCII or non-letter chars pass through.
		j := i
		for j < len(s) {
			c := s[j]
			if !isWordByte(c) {
				break
			}
			j++
		}
		if j > i {
			word := s[i:j]
			if shouldTitleCase(word) {
				out = append(out, byte(toUpper(word[0])))
				for k := 1; k < len(word); k++ {
					out = append(out, byte(toLower(word[k])))
				}
			} else {
				out = append(out, word...)
			}
			i = j
			continue
		}
		out = append(out, s[i])
		i++
	}
	return string(out)
}

func isWordByte(c byte) bool {
	return (c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '\''
}

func shouldTitleCase(w string) bool {
	hasUpper := false
	for i := 0; i < len(w); i++ {
		c := w[i]
		switch {
		case c >= 'A' && c <= 'Z':
			hasUpper = true
		case c >= 'a' && c <= 'z':
			// Already has lowercase — leave the word as is.
			return false
		case c >= '0' && c <= '9' || c == '\'':
			// Allowed.
		default:
			return false
		}
	}
	return hasUpper
}

func toUpper(c byte) byte {
	if c >= 'a' && c <= 'z' {
		return c - 32
	}
	return c
}

func toLower(c byte) byte {
	if c >= 'A' && c <= 'Z' {
		return c + 32
	}
	return c
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
// "パイたばこ" or "パイサたばこ" when Gemini misread the rotated header.
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
