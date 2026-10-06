// Package source discovers PDF URLs from 財務省 and the Wayback Machine.
package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yeighta/flavor-authorization/internal/model"
)

const (
	IndexURL      = "https://www.mof.go.jp/policy/tab_salt/topics/kouriteika.html"
	WaybackPrefix = "https://web.archive.org/web/"
	UserAgent     = "flavor-authorization-bot/0.1 (+https://github.com/yeighta/flavor-authorization)"
)

// pdfRe matches both shinki and henkou file names. Observed variants: suffixes
// "_1" / "1" (20230901_kouriteika1.pdf), the typo "kouritaika", and a 9-digit
// date typo (202606011_kouriteikahenkou.pdf, whose first 8 digits are the date).
var pdfRe = regexp.MustCompile(`(\d{8,9})_kourit[ae]ika(?:henkou)?_?\d*\.pdf`)

// hrefRe captures every link target that points at one of those PDFs. Links on
// the index are relative and not all in the same directory ("./x.pdf" next to
// the index, "../x.pdf" one level up), so they must be resolved, not assumed.
var hrefRe = regexp.MustCompile(`href="([^"]*\d{8,9}_kourit[ae]ika[^"]*\.pdf)"`)

// Collector orchestrates URL discovery.
type Collector struct {
	HTTP *http.Client
}

func NewCollector() *Collector {
	return &Collector{HTTP: &http.Client{Timeout: 60 * time.Second}}
}

// CollectAll fetches the current index and given Wayback snapshot timestamps,
// returning a deduped, chronologically ordered list of PDFRef.
func (c *Collector) CollectAll(ctx context.Context, waybackYears []string) ([]model.PDFRef, error) {
	seen := map[string]model.PDFRef{}

	// 1. Live index
	if links, err := c.fetchPDFLinks(ctx, IndexURL); err == nil {
		for _, u := range links {
			if ref, ok := buildRef(u, ""); ok {
				seen[ref.Filename] = ref
			}
		}
	} else {
		return nil, fmt.Errorf("fetch live index: %w", err)
	}

	// 2. Wayback snapshots
	for _, y := range waybackYears {
		snapURL := WaybackPrefix + y + "/" + IndexURL
		links, err := c.fetchPDFLinks(ctx, snapURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: wayback %s: %v\n", y, err)
			continue
		}
		for _, u := range links {
			orig := unwrapWayback(u)
			wb := WaybackPrefix + y + "/" + orig
			if existing, ok := seen[path.Base(orig)]; ok {
				if existing.WaybackURL == "" {
					existing.WaybackURL = wb
					seen[existing.Filename] = existing
				}
				continue
			}
			// Not in live index → primary URL likely 404, use Wayback as primary fallback.
			if ref, ok := buildRef(orig, wb); ok {
				seen[ref.Filename] = ref
			}
		}
	}

	out := make([]model.PDFRef, 0, len(seen))
	for _, r := range seen {
		out = append(out, r)
	}
	// Sort by (Date, Filename) so the output JSON is fully deterministic.
	// Date alone is not unique when a single day has multiple PDFs.
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date < out[j].Date
		}
		return out[i].Filename < out[j].Filename
	})
	return out, nil
}

// fetchPDFLinks returns the absolute URLs of every kouriteika PDF linked from page.
func (c *Collector) fetchPDFLinks(ctx context.Context, page string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, page, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("http %d", resp.StatusCode)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	return extractPDFLinks(page, string(body))
}

func extractPDFLinks(page, html string) ([]string, error) {
	base, err := url.Parse(page)
	if err != nil {
		return nil, err
	}
	uniq := map[string]struct{}{}
	for _, m := range hrefRe.FindAllStringSubmatch(html, -1) {
		ref, err := url.Parse(m[1])
		if err != nil {
			continue
		}
		uniq[base.ResolveReference(ref).String()] = struct{}{}
	}
	out := make([]string, 0, len(uniq))
	for k := range uniq {
		out = append(out, k)
	}
	sort.Strings(out)
	return out, nil
}

// unwrapWayback turns a Wayback-rewritten link
// (https://web.archive.org/web/2023.../https://www.mof.go.jp/...) back into the original URL.
func unwrapWayback(u string) string {
	if i := strings.Index(u, "/https://"); i >= 0 && strings.HasPrefix(u, WaybackPrefix) {
		return u[i+1:]
	}
	if i := strings.Index(u, "/http://"); i >= 0 && strings.HasPrefix(u, WaybackPrefix) {
		return "https://" + u[i+len("/http://"):]
	}
	return u
}

func buildRef(primaryURL, waybackURL string) (model.PDFRef, bool) {
	filename := path.Base(primaryURL)
	m := pdfRe.FindStringSubmatch(filename)
	if len(m) < 2 {
		return model.PDFRef{}, false
	}
	rawDate := m[1][:8] // YYYYMMDD (a 9-digit typo keeps its first 8 digits)
	date := rawDate[0:4] + "-" + rawDate[4:6] + "-" + rawDate[6:8]

	kind := model.KindShinki
	if strings.Contains(filename, "henkou") {
		kind = model.KindHenkou
	}

	return model.PDFRef{
		Date:       date,
		Kind:       kind,
		URL:        primaryURL,
		WaybackURL: waybackURL,
		Filename:   filename,
	}, true
}

// SaveJSON writes refs as pretty JSON.
func SaveJSON(path string, refs []model.PDFRef) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	return enc.Encode(refs)
}
