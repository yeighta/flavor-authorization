// collect-urls discovers all known PDF URLs (財務省 + Wayback Machine)
// and writes them to data/pdf-urls.json.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/yeighta/flavor-authorization/internal/source"
)

func main() {
	out := flag.String("out", "data/pdf-urls.json", "output JSON path")
	flag.Parse()

	// Wayback snapshot timestamps. We pick yearly anchors that we know exist.
	// These page snapshots cover progressively older PDFs.
	years := []string{
		"20230101",
		"20220101",
		"20210101",
		"20200101",
		"20190101",
		"20181201",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	c := source.NewCollector()
	refs, err := c.CollectAll(ctx, years)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if err := source.SaveJSON(*out, refs); err != nil {
		fmt.Fprintln(os.Stderr, "save:", err)
		os.Exit(1)
	}
	fmt.Printf("wrote %d PDF refs → %s\n", len(refs), *out)
}
