package subquery

import "testing"

func TestAppend(t *testing.T) {
	raw := `[{"key":"os","value":"Android"},{"key":"app","value":"v2rayNG 1.0"}]`
	cases := []struct{ name, link, raw, want string }{
		{"adds to a plain link", "https://h/sub/abc", raw, "https://h/sub/abc?os=Android&app=v2rayNG+1.0"},
		{"keeps an existing query", "https://h/sub/abc?x=1", raw, "https://h/sub/abc?x=1&os=Android&app=v2rayNG+1.0"},
		{"does not override a present key", "https://h/sub/abc?os=iOS", raw, "https://h/sub/abc?os=iOS&app=v2rayNG+1.0"},
		{"empty link stays empty", "", raw, ""},
		{"no setting", "https://h/sub/abc", "", "https://h/sub/abc"},
		{"malformed setting is ignored", "https://h/sub/abc", "not json", "https://h/sub/abc"},
	}
	for _, c := range cases {
		if got := Append(c.link, c.raw); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestValidate(t *testing.T) {
	good := []string{"", "[]", `[{"key":"hwid","value":"abc123"}]`}
	for _, g := range good {
		if err := Validate(g); err != nil {
			t.Errorf("%q should be valid: %v", g, err)
		}
	}
	bad := []string{
		"{}", `[{"key":"","value":"x"}]`, `[{"key":"a b","value":"x"}]`,
		`[{"key":"html","value":"1"}]`, `[{"key":"a","value":"1"},{"key":"a","value":"2"}]`,
	}
	for _, b := range bad {
		if err := Validate(b); err == nil {
			t.Errorf("%q should be rejected", b)
		}
	}
}

func TestParseSkipsInvalid(t *testing.T) {
	got := Parse(`[{"key":"ok","value":"1"},{"key":"bad key","value":"2"},{"key":"ok","value":"3"}]`)
	if len(got) != 1 || got[0].Key != "ok" || got[0].Value != "1" {
		t.Errorf("got %+v", got)
	}
}
