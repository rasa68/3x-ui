// Package subquery handles the admin-configured extra query parameters that
// the panel appends to every subscription link it hands out.
package subquery

import (
	"encoding/json"
	"fmt"
	"net/url"
	"regexp"
	"strings"
)

const (
	MaxParams   = 12
	MaxValueLen = 128
)

// Param is one key/value pair of the setting, stored as a JSON array.
type Param struct {
	Key   string `json:"key"`
	Value string `json:"value"`
}

var (
	keyRe = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	// Used by the subscription server itself; a link carrying them would not return configs.
	reservedKeys = map[string]struct{}{"html": {}, "view": {}, "format": {}}
)

// Parse reads the stored setting leniently: malformed JSON gives no params and
// invalid entries are skipped, so a bad value never breaks link generation.
func Parse(raw string) []Param {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var in []Param
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return nil
	}
	out := make([]Param, 0, len(in))
	seen := make(map[string]struct{}, len(in))
	for _, p := range in {
		p.Key = strings.TrimSpace(p.Key)
		p.Value = strings.TrimSpace(p.Value)
		if checkParam(p) != nil {
			continue
		}
		if _, dup := seen[p.Key]; dup {
			continue
		}
		seen[p.Key] = struct{}{}
		out = append(out, p)
		if len(out) == MaxParams {
			break
		}
	}
	return out
}

func checkParam(p Param) error {
	if !keyRe.MatchString(p.Key) {
		return fmt.Errorf("query parameter name %q is not valid (letters, digits, - and _ only, up to 32)", p.Key)
	}
	if _, bad := reservedKeys[strings.ToLower(p.Key)]; bad {
		return fmt.Errorf("query parameter name %q is reserved", p.Key)
	}
	if len([]rune(p.Value)) > MaxValueLen {
		return fmt.Errorf("value of query parameter %q is longer than %d characters", p.Key, MaxValueLen)
	}
	return nil
}

// Validate checks the setting before it is saved.
func Validate(raw string) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var in []Param
	if err := json.Unmarshal([]byte(raw), &in); err != nil {
		return fmt.Errorf("subscription query parameters must be a JSON array of {key, value}: %w", err)
	}
	if len(in) > MaxParams {
		return fmt.Errorf("at most %d subscription query parameters are allowed", MaxParams)
	}
	seen := make(map[string]struct{}, len(in))
	for _, p := range in {
		p.Key = strings.TrimSpace(p.Key)
		p.Value = strings.TrimSpace(p.Value)
		if err := checkParam(p); err != nil {
			return err
		}
		if _, dup := seen[p.Key]; dup {
			return fmt.Errorf("query parameter %q is listed twice", p.Key)
		}
		seen[p.Key] = struct{}{}
	}
	return nil
}

// Append adds the configured parameters to a subscription link. A parameter
// the link already carries is left alone.
func Append(link, raw string) string {
	if link == "" {
		return link
	}
	params := Parse(raw)
	if len(params) == 0 {
		return link
	}
	base, query, _ := strings.Cut(link, "?")
	existing, _ := url.ParseQuery(query)
	parts := make([]string, 0, len(params))
	for _, p := range params {
		if _, has := existing[p.Key]; has {
			continue
		}
		parts = append(parts, url.QueryEscape(p.Key)+"="+url.QueryEscape(p.Value))
	}
	if len(parts) == 0 {
		return link
	}
	extra := strings.Join(parts, "&")
	if query == "" {
		return base + "?" + extra
	}
	return base + "?" + query + "&" + extra
}
