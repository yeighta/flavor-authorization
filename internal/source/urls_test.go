package source

import (
	"reflect"
	"testing"
)

func TestExtractPDFLinksResolvesRelativeHrefs(t *testing.T) {
	html := `
<a href="./20261002_kouriteika.pdf">a</a>
<a href="../20250826_kouriteika.pdf">b</a>
<a href="./202606011_kouriteikahenkou.pdf">c</a>
<a href="./20230901_kouriteika1.pdf">d</a>
<a href="./20230901_kouriteika1.pdf">dup</a>`
	got, err := extractPDFLinks(IndexURL, html)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		"https://www.mof.go.jp/policy/tab_salt/20250826_kouriteika.pdf",
		"https://www.mof.go.jp/policy/tab_salt/topics/20230901_kouriteika1.pdf",
		"https://www.mof.go.jp/policy/tab_salt/topics/202606011_kouriteikahenkou.pdf",
		"https://www.mof.go.jp/policy/tab_salt/topics/20261002_kouriteika.pdf",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %v\nwant %v", got, want)
	}
}

func TestBuildRef(t *testing.T) {
	cases := map[string]struct{ date, kind string }{
		"https://www.mof.go.jp/policy/tab_salt/topics/202606011_kouriteikahenkou.pdf": {"2026-06-01", "henkou"},
		"https://www.mof.go.jp/policy/tab_salt/topics/20230901_kouriteika1.pdf":       {"2023-09-01", "shinki"},
		"https://www.mof.go.jp/policy/tab_salt/20250121_kouriteika_1.pdf":             {"2025-01-21", "shinki"},
	}
	for u, want := range cases {
		ref, ok := buildRef(u, "")
		if !ok || ref.Date != want.date || string(ref.Kind) != want.kind || ref.URL != u {
			t.Errorf("buildRef(%s) = %+v, %v", u, ref, ok)
		}
	}
}

func TestUnwrapWayback(t *testing.T) {
	in := "https://web.archive.org/web/20230101000000/https://www.mof.go.jp/policy/tab_salt/topics/20200907_kouriteikahenkou.pdf"
	if got, want := unwrapWayback(in), "https://www.mof.go.jp/policy/tab_salt/topics/20200907_kouriteikahenkou.pdf"; got != want {
		t.Errorf("got %s", got)
	}
}
