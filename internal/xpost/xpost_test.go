package xpost

import "testing"

// Vector from X's "Creating a signature" documentation.
func TestSignature(t *testing.T) {
	c := Credentials{
		APISecret:         "kAcSOqF21Fu85e7zjz7ZN2U4ZRhfV3WpwPAoE3Z7kBw",
		AccessTokenSecret: "LswwdoUaIvS8ltyTt5jkRh4J50vUPVVHtR2YPi5kE",
	}
	params := map[string]string{
		"status":                 "Hello Ladies + Gentlemen, a signed OAuth request!",
		"include_entities":       "true",
		"oauth_consumer_key":     "xvz1evFS4wEEPTGEFPHBog",
		"oauth_nonce":            "kYjzVBB8Y0ZFabxSWbWovY3uYSQ2pTgmZeNu2VS4cg",
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        "1318622958",
		"oauth_token":            "370773112-GmHxMAgYyLbNEtIKZeRNFsMKPR9EyMZeS9weJAEb",
		"oauth_version":          "1.0",
	}
	got := signature(c, "POST", "https://api.twitter.com/1.1/statuses/update.json", params)
	if want := "hCtSmYh+iHYCEqBWrE7C7hYmtUk="; got != want {
		t.Fatalf("signature = %q, want %q", got, want)
	}
}

func TestWeightedLength(t *testing.T) {
	cases := map[string]int{
		"abc":                         3,
		"あいう":                         6,
		"see https://example.com/x/y": 4 + 23,
	}
	for in, want := range cases {
		if got := WeightedLength(in); got != want {
			t.Errorf("WeightedLength(%q) = %d, want %d", in, got, want)
		}
	}
}
