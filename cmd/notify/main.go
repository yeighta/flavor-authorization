// notify diffs the previous products / manufacturers DB against the current one
// and announces new products and price revisions to Discord and X (Twitter).
// Each channel is skipped silently when its credentials are not configured.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/yeighta/flavor-authorization/internal/classifier"
	"github.com/yeighta/flavor-authorization/internal/model"
	"github.com/yeighta/flavor-authorization/internal/xpost"
)

type priceChange struct {
	Old model.Product
	New model.Product
}

type diff struct {
	Added         []model.Product
	PriceChanges  []priceChange
	Manufacturers []string
}

func (d diff) empty() bool {
	return len(d.Added) == 0 && len(d.PriceChanges) == 0 && len(d.Manufacturers) == 0
}

func main() {
	oldProducts := flag.String("old-products", "", "previous products.json (missing file = empty)")
	newProducts := flag.String("new-products", "data/products.json", "current products.json")
	oldMfrs := flag.String("old-manufacturers", "", "previous manufacturers.json (missing file = empty)")
	newMfrs := flag.String("new-manufacturers", "data/manufacturers.json", "current manufacturers.json")
	prURL := flag.String("pr-url", "", "PR URL linked from the Discord embed")
	dryRun := flag.Bool("dry-run", false, "print messages instead of sending")
	flag.Parse()

	_ = godotenv.Load()

	d := computeDiff(
		loadProducts(*oldProducts), loadProducts(*newProducts),
		loadManufacturers(*oldMfrs), loadManufacturers(*newMfrs),
	)
	log.Printf("diff: added=%d priceChanges=%d manufacturers=%d", len(d.Added), len(d.PriceChanges), len(d.Manufacturers))
	if d.empty() {
		log.Printf("nothing to announce")
		return
	}

	siteURL := os.Getenv("SITE_URL")
	if siteURL == "" {
		siteURL = "https://flavor-authorization.pages.dev"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	failed := false

	discordDesc := discordDescription(d)
	if *dryRun {
		fmt.Printf("=== Discord ===\n%s\n\n", discordDesc)
	} else if hook := os.Getenv("DISCORD_WEBHOOK_URL"); hook == "" {
		log.Printf("DISCORD_WEBHOOK_URL not set; skipping Discord")
	} else if err := postDiscord(ctx, hook, discordDesc, *prURL); err != nil {
		log.Printf("discord: %v", err)
		failed = true
	} else {
		log.Printf("discord: sent")
	}

	// Manufacturer-only changes aren't interesting to followers.
	if len(d.Added) > 0 || len(d.PriceChanges) > 0 {
		tweet := tweetText(d, siteURL)
		creds := xpost.CredentialsFromEnv()
		if *dryRun {
			fmt.Printf("=== X (%d/280) ===\n%s\n", xpost.WeightedLength(tweet), tweet)
		} else if !creds.Valid() {
			log.Printf("X_* credentials not set; skipping X")
		} else if id, err := xpost.Post(ctx, creds, tweet); err != nil {
			log.Printf("x: %v", err)
			failed = true
		} else {
			log.Printf("x: posted %s", id)
		}
	}

	if failed {
		os.Exit(1)
	}
}

func computeDiff(oldP, newP []model.Product, oldM, newM map[string]classifier.Classification) diff {
	oldByKey := make(map[string]model.Product, len(oldP))
	for _, p := range oldP {
		oldByKey[p.Key()] = p
	}
	var d diff
	for _, p := range newP {
		prev, ok := oldByKey[p.Key()]
		switch {
		case !ok:
			d.Added = append(d.Added, p)
		case prev.PriceYen != p.PriceYen:
			d.PriceChanges = append(d.PriceChanges, priceChange{Old: prev, New: p})
		}
	}
	for name := range newM {
		if _, ok := oldM[name]; !ok {
			d.Manufacturers = append(d.Manufacturers, name)
		}
	}
	sort.Strings(d.Manufacturers)
	return d
}

func productLabel(p model.Product) string {
	s := p.Manufacturer + " " + p.Name
	if p.Grams != "" {
		s += " " + p.Grams
	}
	return s
}

func yen(n int) string {
	s := fmt.Sprint(n)
	var b strings.Builder
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return "¥" + b.String()
}

// discordDescription lists up to 10 items per section; embed descriptions cap at 4096 chars.
func discordDescription(d diff) string {
	const maxItems = 10
	var b strings.Builder
	section := func(title string, n int, lines []string) {
		if n == 0 {
			return
		}
		fmt.Fprintf(&b, "**%s**: %d件\n", title, n)
		for i, l := range lines {
			if i == maxItems {
				fmt.Fprintf(&b, "…他 %d 件\n", n-maxItems)
				break
			}
			b.WriteString("・" + l + "\n")
		}
		b.WriteString("\n")
	}
	var added []string
	for _, p := range d.Added {
		added = append(added, fmt.Sprintf("%s %s", productLabel(p), yen(p.PriceYen)))
	}
	section("新規商品", len(d.Added), added)
	var changed []string
	for _, c := range d.PriceChanges {
		changed = append(changed, fmt.Sprintf("%s %s → %s", productLabel(c.New), yen(c.Old.PriceYen), yen(c.New.PriceYen)))
	}
	section("価格改定", len(d.PriceChanges), changed)
	section("新規メーカー", len(d.Manufacturers), d.Manufacturers)
	out := strings.TrimSpace(b.String())
	if r := []rune(out); len(r) > 4000 {
		out = string(r[:4000]) + "…"
	}
	return out
}

// countByManufacturer returns "MFR(n)" entries ordered by count desc, then name.
func countByManufacturer(names []string) []string {
	counts := map[string]int{}
	for _, n := range names {
		counts[n]++
	}
	keys := make([]string, 0, len(counts))
	for k := range counts {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if counts[keys[i]] != counts[keys[j]] {
			return counts[keys[i]] > counts[keys[j]]
		}
		return keys[i] < keys[j]
	})
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = fmt.Sprintf("%s(%d)", k, counts[k])
	}
	return out
}

// tweetText builds a single post that fits X's 280 weighted-char limit,
// dropping trailing manufacturers into "他N社" as needed.
func tweetText(d diff, siteURL string) string {
	var addedM, changedM []string
	for _, p := range d.Added {
		addedM = append(addedM, p.Manufacturer)
	}
	for _, c := range d.PriceChanges {
		changedM = append(changedM, c.New.Manufacturer)
	}
	added := countByManufacturer(addedM)
	changed := countByManufacturer(changedM)

	build := func(nAdded, nChanged int) string {
		var b strings.Builder
		b.WriteString("【シーシャ認可情報】\n")
		line := func(title string, total int, items []string, n int) {
			if total == 0 {
				return
			}
			fmt.Fprintf(&b, "\n%s %d件\n", title, total)
			b.WriteString(strings.Join(items[:n], " / "))
			if rest := len(items) - n; rest > 0 {
				if n > 0 {
					b.WriteString(" ")
				}
				fmt.Fprintf(&b, "ほか%d社", rest)
			}
			b.WriteString("\n")
		}
		line("🆕 新規", len(d.Added), added, nAdded)
		line("💴 価格改定", len(d.PriceChanges), changed, nChanged)
		b.WriteString("\n" + siteURL)
		return b.String()
	}

	// Shrink the longer list first until it fits.
	nA, nC := len(added), len(changed)
	for {
		t := build(nA, nC)
		if xpost.WeightedLength(t) <= 280 || (nA == 0 && nC == 0) {
			return t
		}
		if nA >= nC && nA > 0 {
			nA--
		} else {
			nC--
		}
	}
}

func postDiscord(ctx context.Context, hook, desc, url string) error {
	embed := map[string]any{
		"title":       "認可たばこDB 更新",
		"description": desc,
		"color":       3066993,
	}
	if url != "" {
		embed["url"] = url
	}
	body, _ := json.Marshal(map[string]any{"embeds": []any{embed}})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, hook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode/100 != 2 {
		raw, _ := io.ReadAll(res.Body)
		return fmt.Errorf("HTTP %d: %s", res.StatusCode, raw)
	}
	return nil
}

func loadProducts(path string) []model.Product {
	var ps []model.Product
	if path == "" {
		return ps
	}
	f, err := os.Open(path)
	if err != nil {
		return ps
	}
	defer f.Close()
	if err := json.NewDecoder(f).Decode(&ps); err != nil {
		log.Fatalf("decode %s: %v", path, err)
	}
	return ps
}

func loadManufacturers(path string) map[string]classifier.Classification {
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	var m classifier.Map
	if err := json.NewDecoder(f).Decode(&m); err != nil {
		log.Fatalf("decode %s: %v", path, err)
	}
	return m.Entries
}
