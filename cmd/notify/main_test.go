package main

import (
	"testing"

	"github.com/yeighta/flavor-authorization/internal/model"
)

func p(name string, price int, date string) model.Product {
	return model.Product{Category: model.CategoryPipe, Manufacturer: "BALLI", Name: name, Grams: "50.0g", PriceYen: price, UpdatedDate: date}
}

func TestComputeDiffAnnouncesOnlyNewNotices(t *testing.T) {
	old := []model.Product{p("Apple", 1600, "2022-02-17"), p("Lemon", 1600, "2026-09-04")}
	cur := []model.Product{
		p("Apple", 1500, "2026-10-02"),  // revised in a new notice → announce
		p("Lemon", 1600, "2026-09-04"),  // unchanged
		p("Mint", 1500, "2026-10-02"),   // new in a new notice → announce
		p("Banana", 1600, "2021-05-01"), // old row that only surfaced via re-merge → skip
	}
	d := computeDiff(old, cur, nil, nil)
	if len(d.Added) != 1 || d.Added[0].Name != "Mint" {
		t.Errorf("Added = %v, want only Mint", d.Added)
	}
	if len(d.PriceChanges) != 1 || d.PriceChanges[0].New.Name != "Apple" {
		t.Errorf("PriceChanges = %v, want only Apple", d.PriceChanges)
	}
	// Running again against the merged result must announce nothing.
	if again := computeDiff(cur, cur, nil, nil); !again.empty() {
		t.Errorf("second run announced %+v", again)
	}
}
