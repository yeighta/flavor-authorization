// Package pdf handles downloading PDFs from 財務省 with Wayback Machine fallback.
package pdf

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/yeighta/flavor-authorization/internal/model"
)

const UserAgent = "flavor-authorization-bot/0.1 (+https://github.com/yeighta/flavor-authorization)"

type Fetcher struct {
	HTTP *http.Client
}

func NewFetcher() *Fetcher {
	return &Fetcher{HTTP: &http.Client{Timeout: 45 * time.Second}}
}

// Fetch tries to download a PDF for ref to dst. Strategy:
//   1. ref.URL (live 財務省) — only attempted if Wayback URL absent OR primary is "fresh enough"
//   2. ref.WaybackURL (transformed to "id_" raw modifier) — known archive
// Returns the URL actually used.
func (f *Fetcher) Fetch(ctx context.Context, ref model.PDFRef, dst string) (string, error) {
	tried := []string{}

	// If we have a known Wayback URL, the live URL is presumed dead — skip it to save a roundtrip.
	if ref.WaybackURL == "" {
		tried = append(tried, ref.URL)
		if err := f.tryFetch(ctx, ref.URL, dst); err == nil {
			return ref.URL, nil
		}
	} else {
		// Live first, since archived files are sometimes incomplete; fall back to Wayback if 404.
		tried = append(tried, ref.URL)
		if err := f.tryFetch(ctx, ref.URL, dst); err == nil {
			return ref.URL, nil
		}
	}

	if ref.WaybackURL != "" {
		raw := toRawWayback(ref.WaybackURL)
		tried = append(tried, raw)
		if err := f.tryFetch(ctx, raw, dst); err == nil {
			return raw, nil
		}
	}

	return "", fmt.Errorf("all sources failed (tried: %s)", strings.Join(tried, ", "))
}

// FetchURL downloads a single URL to dst (no fallback). Used for diagnostics.
func (f *Fetcher) FetchURL(ctx context.Context, url, dst string) (string, error) {
	if err := f.tryFetch(ctx, url, dst); err != nil {
		return "", err
	}
	return url, nil
}

// waybackTimestampRe matches the timestamp portion of a Wayback URL, with or
// without the "id_" / "if_" / "im_" suffix.
var waybackTimestampRe = regexp.MustCompile(`^(https://web\.archive\.org/web/\d+)([a-z]+_)?(/.+)$`)

// toRawWayback ensures the URL uses the "id_" modifier so we get the original archived
// bytes rather than the Wayback Machine's HTML wrapper.
func toRawWayback(u string) string {
	m := waybackTimestampRe.FindStringSubmatch(u)
	if m == nil {
		return u
	}
	return m[1] + "id_" + m[3]
}

func (f *Fetcher) tryFetch(ctx context.Context, url, dst string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", UserAgent)
	resp, err := f.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("status %d", resp.StatusCode)
	}
	ct := resp.Header.Get("Content-Type")
	if !strings.Contains(ct, "pdf") && !strings.Contains(ct, "octet-stream") {
		return fmt.Errorf("not a pdf: content-type=%s", ct)
	}
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, resp.Body)
	return err
}
