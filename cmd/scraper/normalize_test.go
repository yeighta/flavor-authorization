package main

import "testing"

func TestNormalizeProductName(t *testing.T) {
	cases := map[string]string{
		"ALOHA NIGHTS":                 "Aloha Nights",
		"Aloha nights":                 "Aloha Nights",
		"Blueberry With Mint":          "Blueberry with Mint",
		"cherry with mint":             "Cherry with Mint",
		"Elite Edition ICE Mint":       "Elite Edition Ice Mint",
		"JSE Double Apple":             "JSE Double Apple",
		"Black VALENTINE's":            "Black Valentine's",
		"Granny’s Kiss":                "Granny's Kiss",
		"Gold Line Barista ' s Choice": "Gold Line Barista's Choice",
		"MexiCola":                     "MexiCola",
		"ｱｲｽ ﾐﾝﾄ":                      "アイス ミント",
		"Ｄｏｕｂｌｅ　Ａｐｐｌｅ":                 "Double Apple",
		"Water  melon":                 "Water Melon",
		"CRÈME BRÛLÉE":                 "Crème Brûlée",
		"Crème caramel":                "Crème Caramel",
	}
	for in, want := range cases {
		if got := normalizeProductName(in); got != want {
			t.Errorf("normalizeProductName(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNormalizeCountryAndVariant(t *testing.T) {
	for in, want := range map[string]string{"ｱﾒﾘｶ合衆＿国": "アメリカ合衆国", "ﾛｼｱ　ﾓﾙﾄﾞﾊﾞ": "ロシア・モルドバ"} {
		if got := normalizeCountry(in); got != want {
			t.Errorf("normalizeCountry(%q) = %q, want %q", in, got, want)
		}
	}
	for in, want := range map[string]string{"ﾌﾟﾗｹｰｽ": "プラケース", "100.0g 箱": "箱", "１００．０ｇ　缶": "缶"} {
		if got := normalizeVariant(in); got != want {
			t.Errorf("normalizeVariant(%q) = %q, want %q", in, got, want)
		}
	}
}
