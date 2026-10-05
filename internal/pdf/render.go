package pdf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// RenderPNG rasterizes every page with poppler's pdftoppm, for vision models that
// don't take PDFs directly. We deliberately render instead of reading the text
// layer: many 財務省 PDFs (most of 2025–) ship broken ToUnicode maps, so their
// text layer is garbage even though the glyphs render correctly.
// 200dpi keeps the small half-width katakana in 製品の区分 (ﾊﾞｯｸﾞ etc.) legible.
func RenderPNG(ctx context.Context, pdfPath string, dpi int) ([][]byte, error) {
	dir, err := os.MkdirTemp("", "pages-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(dir)

	var stderr bytes.Buffer
	cmd := exec.CommandContext(ctx, "pdftoppm", "-r", strconv.Itoa(dpi), "-png", pdfPath, filepath.Join(dir, "p"))
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("pdftoppm: %w (%s)", err, strings.TrimSpace(stderr.String()))
	}
	files, err := filepath.Glob(filepath.Join(dir, "p-*.png"))
	if err != nil {
		return nil, err
	}
	// pdftoppm zero-pads page numbers to a common width, so lexical order is page order.
	sort.Strings(files)
	pages := make([][]byte, 0, len(files))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		pages = append(pages, b)
	}
	if len(pages) == 0 {
		return nil, fmt.Errorf("pdftoppm produced no pages")
	}
	return pages, nil
}
