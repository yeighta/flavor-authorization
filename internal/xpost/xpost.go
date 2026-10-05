// Package xpost publishes a post to X (Twitter) via the v2 API using OAuth 1.0a
// user-context credentials. No third-party dependency: the signature is built
// by hand (HMAC-SHA1 over the oauth_* params; the JSON body is not signed).
package xpost

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const Endpoint = "https://api.x.com/2/tweets"

// Credentials are the four values from the X developer portal
// (app "Keys and tokens", with Read and Write permission).
type Credentials struct {
	APIKey            string
	APISecret         string
	AccessToken       string
	AccessTokenSecret string
}

func CredentialsFromEnv() Credentials {
	return Credentials{
		APIKey:            os.Getenv("X_API_KEY"),
		APISecret:         os.Getenv("X_API_SECRET"),
		AccessToken:       os.Getenv("X_ACCESS_TOKEN"),
		AccessTokenSecret: os.Getenv("X_ACCESS_TOKEN_SECRET"),
	}
}

func (c Credentials) Valid() bool {
	return c.APIKey != "" && c.APISecret != "" && c.AccessToken != "" && c.AccessTokenSecret != ""
}

// Post creates a post and returns its ID.
func Post(ctx context.Context, c Credentials, text string) (string, error) {
	body, _ := json.Marshal(map[string]string{"text": text})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, Endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", authorization(c, http.MethodPost, Endpoint))

	res, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	if res.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("HTTP %d: %s", res.StatusCode, raw)
	}
	var parsed struct {
		Data struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", fmt.Errorf("decode response: %w", err)
	}
	return parsed.Data.ID, nil
}

func authorization(c Credentials, method, endpoint string) string {
	nonce := make([]byte, 16)
	_, _ = rand.Read(nonce)
	params := map[string]string{
		"oauth_consumer_key":     c.APIKey,
		"oauth_nonce":            hex.EncodeToString(nonce),
		"oauth_signature_method": "HMAC-SHA1",
		"oauth_timestamp":        strconv.FormatInt(time.Now().Unix(), 10),
		"oauth_token":            c.AccessToken,
		"oauth_version":          "1.0",
	}
	params["oauth_signature"] = signature(c, method, endpoint, params)

	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	parts := make([]string, len(keys))
	for i, k := range keys {
		parts[i] = fmt.Sprintf(`%s="%s"`, k, escape(params[k]))
	}
	return "OAuth " + strings.Join(parts, ", ")
}

func signature(c Credentials, method, endpoint string, params map[string]string) string {
	keys := make([]string, 0, len(params))
	for k := range params {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	pairs := make([]string, len(keys))
	for i, k := range keys {
		pairs[i] = escape(k) + "=" + escape(params[k])
	}
	base := method + "&" + escape(endpoint) + "&" + escape(strings.Join(pairs, "&"))
	key := escape(c.APISecret) + "&" + escape(c.AccessTokenSecret)
	mac := hmac.New(sha1.New, []byte(key))
	mac.Write([]byte(base))
	return base64.StdEncoding.EncodeToString(mac.Sum(nil))
}

// escape is RFC 3986 percent-encoding as required by OAuth 1.0a.
func escape(s string) string {
	return strings.ReplaceAll(url.QueryEscape(s), "+", "%20")
}

var urlRe = regexp.MustCompile(`https?://\S+`)

// WeightedLength approximates X's counting rules: URLs count as 23, code points in
// the Latin/general-punctuation ranges count as 1, everything else (CJK, emoji) as 2.
func WeightedLength(s string) int {
	n := 0
	s = urlRe.ReplaceAllStringFunc(s, func(string) string {
		n += 23
		return ""
	})
	for _, r := range s {
		switch {
		case r <= 0x10FF,
			r >= 0x2000 && r <= 0x200D,
			r >= 0x2010 && r <= 0x201F,
			r >= 0x2032 && r <= 0x2037:
			n++
		default:
			n += 2
		}
	}
	return n
}
