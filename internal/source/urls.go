// Package source discovers PDF URLs from 財務省 and the Wayback Machine.
package source

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/yeighta/flavor-authorization/internal/model"
)

const (
	IndexURL      = "https://www.mof.go.jp/policy/tab_salt/topics/kouriteika.html"
	IndexBase     = "https://www.mof.go.jp/policy/tab_salt/topics/"
	WaybackPrefix = "https://web.archive.org/web/"
	UserAgent     = "flavor-authorization-bot/0.1 (+https://github.com/yeighta/flavor-authorization)"
)

// pdfRe matches both shinki and henkou file names.
// Some 2018 files have suffixes like _1, _2 and a typo "kouritaika".
var pdfRe = regexp.MustCompile(`(\d{8})_kourit[ae]ika(?:henkou)?(?:_\d+)?\.pdf`)

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
	if filenames, err := c.fetchPDFFilenames(ctx, IndexURL); err == nil {
		for _, f := range filenames {
			ref, ok := buildRef(f, IndexBase+f, "")
			if ok {
				seen[f] = ref
			}
		}
	} else {
		return nil, fmt.Errorf("fetch live index: %w", err)
	}

	// 2. Wayback snapshots
	for _, y := range waybackYears {
		snapURL := WaybackPrefix + y + "/" + IndexURL
		filenames, err := c.fetchPDFFilenames(ctx, snapURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "warn: wayback %s: %v\n", y, err)
			continue
		}
		for _, f := range filenames {
			if existing, ok := seen[f]; ok {
				if existing.WaybackURL == "" {
					existing.WaybackURL = WaybackPrefix + y + "/" + IndexBase + f
					seen[f] = existing
				}
				continue
			}
			// Not in live index → primary URL likely 404, use Wayback as primary fallback.
			ref, ok := buildRef(f, IndexBase+f, WaybackPrefix+y+"/"+IndexBase+f)
			if ok {
				seen[f] = ref
			}
		}
	}

	out := make([]model.PDFRef, 0, len(seen))
	for _, r := range seen {
		out = append(out, r)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Date < out[j].Date })
	return out, nil
}

func (c *Collector) fetchPDFFilenames(ctx context.Context, url string) ([]string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
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
	matches := pdfRe.FindAllString(string(body), -1)
	uniq := map[string]struct{}{}
	for _, m := range matches {
		uniq[m] = struct{}{}
	}
	out := make([]string, 0, len(uniq))
	for k := range uniq {
		out = append(out, k)
	}
	return out, nil
}

func buildRef(filename, primaryURL, waybackURL string) (model.PDFRef, bool) {
	m := pdfRe.FindStringSubmatch(filename)
	if len(m) < 2 {
		return model.PDFRef{}, false
	}
	rawDate := m[1] // YYYYMMDD
	if len(rawDate) != 8 {
		return model.PDFRef{}, false
	}
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
