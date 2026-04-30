// classify reads data/products.json, gathers unique パイプたばこ manufacturers,
// asks Gemini to classify each as kiseru/shisha/unknown, and writes
// data/manufacturers.json. Existing entries are preserved (manual edits win)
// unless --refresh is passed.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/yeighta/flavor-authorization/internal/classifier"
	"github.com/yeighta/flavor-authorization/internal/model"
)

func main() {
	productsPath := flag.String("products", "data/products.json", "input products DB")
	outPath := flag.String("out", "data/manufacturers.json", "manufacturer classification output")
	refresh := flag.Bool("refresh", false, "re-classify even manufacturers that already have an entry")
	batch := flag.Int("batch", 30, "number of manufacturers per LLM request")
	maxSamples := flag.Int("max-samples", 8, "max product names per manufacturer fed to the LLM")
	flag.Parse()

	_ = godotenv.Load()

	products, err := loadProducts(*productsPath)
	if err != nil {
		log.Fatalf("load products: %v", err)
	}

	samples := buildSamples(products, *maxSamples)
	log.Printf("found %d unique pipe-tobacco manufacturers", len(samples))

	existing := loadMap(*outPath)
	locked := 0
	var todo []classifier.Sample
	for _, s := range samples {
		entry, hasEntry := existing.Entries[s.Manufacturer]
		switch {
		case hasEntry && entry.Locked:
			// Manual override — never overwrite, even with --refresh.
			locked++
		case *refresh:
			todo = append(todo, s)
		case !hasEntry:
			todo = append(todo, s)
		}
	}
	log.Printf("classifying %d / total %d (locked: %d)", len(todo), len(samples), locked)
	if len(todo) == 0 {
		if err := saveMap(*outPath, existing); err != nil {
			log.Fatalf("save: %v", err)
		}
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
	defer cancel()
	c, err := classifier.NewClient(ctx)
	if err != nil {
		log.Fatalf("client: %v", err)
	}

	for i := 0; i < len(todo); i += *batch {
		end := i + *batch
		if end > len(todo) {
			end = len(todo)
		}
		log.Printf("batch %d-%d / %d", i+1, end, len(todo))
		batchResult, err := c.Classify(ctx, todo[i:end])
		if err != nil {
			log.Printf("  ERR: %v (skipping batch)", err)
			continue
		}
		for _, cl := range batchResult {
			existing.Entries[cl.Manufacturer] = cl
		}
		// Persist incrementally so a later failure doesn't lose progress.
		if err := saveMap(*outPath, existing); err != nil {
			log.Fatalf("save: %v", err)
		}
		// Polite pause between batches.
		time.Sleep(4 * time.Second)
	}
	log.Printf("done. wrote %d entries to %s", len(existing.Entries), *outPath)
}

func loadProducts(path string) ([]model.Product, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var ps []model.Product
	return ps, json.NewDecoder(f).Decode(&ps)
}

func buildSamples(products []model.Product, maxNames int) []classifier.Sample {
	type acc struct {
		countries map[string]struct{}
		names     []string
	}
	byMan := map[string]*acc{}
	for _, p := range products {
		if p.Category != model.CategoryPipe {
			continue
		}
		m := strings.TrimSpace(p.Manufacturer)
		if m == "" {
			continue
		}
		a, ok := byMan[m]
		if !ok {
			a = &acc{countries: map[string]struct{}{}}
			byMan[m] = a
		}
		if c := strings.TrimSpace(p.Country); c != "" {
			a.countries[c] = struct{}{}
		}
		if len(a.names) < maxNames {
			a.names = append(a.names, p.Name)
		}
	}
	out := make([]classifier.Sample, 0, len(byMan))
	for m, a := range byMan {
		countries := make([]string, 0, len(a.countries))
		for c := range a.countries {
			countries = append(countries, c)
		}
		sort.Strings(countries)
		out = append(out, classifier.Sample{
			Manufacturer: m,
			Countries:    countries,
			ProductNames: a.names,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Manufacturer < out[j].Manufacturer })
	return out
}

func loadMap(path string) classifier.Map {
	m := classifier.Map{Entries: map[string]classifier.Classification{}}
	f, err := os.Open(path)
	if err != nil {
		return m
	}
	defer f.Close()
	_ = json.NewDecoder(f).Decode(&m)
	if m.Entries == nil {
		m.Entries = map[string]classifier.Classification{}
	}
	return m
}

func saveMap(path string, m classifier.Map) error {
	tmp := path + ".tmp"
	f, err := os.Create(tmp)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(f)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmp, path)
}

// silence import lint if unused
var _ = fmt.Sprintf
