// normalize-manufacturers reads the merged products DB, clusters spelling
// variants of pipe-tobacco manufacturer names via Gemini, and writes
// data/manufacturer-aliases.json. Re-running merge afterwards collapses the
// variants in products.json.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"log"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/yeighta/flavor-authorization/internal/model"
	"github.com/yeighta/flavor-authorization/internal/normalizer"
)

func main() {
	productsPath := flag.String("products", "data/products.json", "input products DB")
	outPath := flag.String("out", "data/manufacturer-aliases.json", "alias map output")
	maxSamples := flag.Int("max-samples", 5, "max product names per manufacturer fed to the LLM")
	flag.Parse()

	_ = godotenv.Load()

	products, err := loadProducts(*productsPath)
	if err != nil {
		log.Fatalf("load products: %v", err)
	}

	samples, counts := buildSamples(products, *maxSamples)
	log.Printf("found %d unique pipe-tobacco manufacturers", len(samples))

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()

	c, err := normalizer.NewClient(ctx)
	if err != nil {
		log.Fatalf("client: %v", err)
	}
	clusters, err := c.Cluster(ctx, samples)
	if err != nil {
		log.Fatalf("cluster: %v", err)
	}
	log.Printf("LLM identified %d clusters", len(clusters))

	aliases := normalizer.AliasMap{Aliases: map[string]string{}}
	for _, cl := range clusters {
		if len(cl.Names) < 2 {
			continue
		}
		canonical := normalizer.PickCanonical(cl.Names, counts)
		for _, n := range cl.Names {
			aliases.Aliases[n] = canonical
		}
		log.Printf("  cluster %d: %v → %q", len(cl.Names), cl.Names, canonical)
	}

	if err := saveAliases(*outPath, aliases); err != nil {
		log.Fatalf("save: %v", err)
	}
	log.Printf("wrote %d alias mappings → %s", len(aliases.Aliases), *outPath)
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

func buildSamples(products []model.Product, maxNames int) ([]normalizer.Sample, map[string]int) {
	type acc struct {
		countries map[string]struct{}
		names     []string
		count     int
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
		a.count++
		if c := strings.TrimSpace(p.Country); c != "" {
			a.countries[c] = struct{}{}
		}
		if len(a.names) < maxNames {
			a.names = append(a.names, p.Name)
		}
	}
	out := make([]normalizer.Sample, 0, len(byMan))
	counts := make(map[string]int, len(byMan))
	for m, a := range byMan {
		countries := make([]string, 0, len(a.countries))
		for c := range a.countries {
			countries = append(countries, c)
		}
		sort.Strings(countries)
		out = append(out, normalizer.Sample{
			Name:         m,
			Countries:    countries,
			ProductNames: a.names,
			Count:        a.count,
		})
		counts[m] = a.count
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, counts
}

func saveAliases(path string, m normalizer.AliasMap) error {
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
