package pdf

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// HasPdftotext reports whether pdftotext is on PATH.
func HasPdftotext() bool {
	_, err := exec.LookPath("pdftotext")
	return err == nil
}

// HasOCRmyPDF reports whether ocrmypdf is on PATH.
func HasOCRmyPDF() bool {
	_, err := exec.LookPath("ocrmypdf")
	return err == nil
}

// ExtractText returns layout-preserving text from a PDF.
// Strategy: try pdftotext first; if the result looks garbled (no expected markers),
// run ocrmypdf to produce a text layer, then run pdftotext again.
//
// Sentinel strings are common section labels that should appear in any well-extracted
// 認可たばこ PDF. Their absence indicates a CID-without-ToUnicode font.
func ExtractText(ctx context.Context, pdfPath string) (text string, usedOCR bool, err error) {
	native, nerr := pdftotextLayout(ctx, pdfPath)
	if nerr == nil && looksReadable(native) {
		return native, false, nil
	}
	// OCR fallback
	if !HasOCRmyPDF() {
		return native, false, fmt.Errorf("native extract not readable and ocrmypdf not installed")
	}
	ocrPath := strings.TrimSuffix(pdfPath, filepath.Ext(pdfPath)) + ".ocr.pdf"
	cmd := exec.CommandContext(ctx,
		"ocrmypdf",
		"--force-ocr",
		"-l", "jpn",
		"--output-type", "pdf",
		"--quiet",
		pdfPath, ocrPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		return native, false, fmt.Errorf("ocrmypdf: %v: %s", err, out)
	}
	defer os.Remove(ocrPath)
	ocred, oerr := pdftotextLayout(ctx, ocrPath)
	if oerr != nil {
		return native, true, oerr
	}
	return ocred, true, nil
}

func pdftotextLayout(ctx context.Context, pdfPath string) (string, error) {
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "pdftotext", "-layout", "-enc", "UTF-8", pdfPath, "-")
	cmd.Stdout = &buf
	cmd.Stderr = nil
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// looksReadable returns true when the text contains at least one expected
// section header in valid Japanese. We check for any of the top-level
// 製造たばこの区分 values.
func looksReadable(s string) bool {
	markers := []string{
		"パイプたばこ",
		"紙巻たばこ",
		"葉巻たばこ",
		"刻みたばこ",
		"製造たばこの区分",
	}
	for _, m := range markers {
		if strings.Contains(s, m) {
			return true
		}
	}
	return false
}
