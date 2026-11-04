package tgbot

import (
	"strings"
	"testing"
)

func TestOnlinePayloadRoundTrip(t *testing.T) {
	payload, ok := encodeOnlinePayload("alice@example")
	if !ok || !strings.HasPrefix(payload, onlinePayloadPrefix) {
		t.Fatalf("payload %q ok=%v", payload, ok)
	}
	if strings.ContainsAny(payload, ".+/=") {
		t.Fatalf("payload %q has characters Telegram rejects", payload)
	}
	email, ok := decodeOnlinePayload(payload)
	if !ok || email != "alice@example" {
		t.Fatalf("decoded %q ok=%v", email, ok)
	}
}

func TestOnlinePayloadLimits(t *testing.T) {
	if _, ok := encodeOnlinePayload(strings.Repeat("a", 60)); ok {
		t.Fatal("an email that does not fit 64 characters must not produce a link")
	}
	// An invite payload (plain base64 of a subId) is never mistaken for an online link.
	invite, _ := encodeInvitePayload("abcd1234efgh5678")
	if _, ok := decodeOnlinePayload(invite); ok {
		t.Fatalf("invite payload %q decoded as an online link", invite)
	}
	if _, ok := decodeOnlinePayload("o_!!!"); ok {
		t.Fatal("garbage after the prefix must be rejected")
	}
	if _, ok := decodeOnlinePayload("o_"); ok {
		t.Fatal("an empty email must be rejected")
	}
}

func TestCheckHostLinks(t *testing.T) {
	if got := checkHostURL("1.2.3.4"); got != "https://check-host.net/ip-info?host=1.2.3.4" {
		t.Errorf("v4: %s", got)
	}
	if got := checkHostURL("2001:db8::1"); got != "https://check-host.net/ip-info?host=2001%3Adb8%3A%3A1" {
		t.Errorf("v6: %s", got)
	}
	want := `<a href="https://check-host.net/ip-info?host=1.2.3.4">1.2.3.4</a>`
	if got := ipLink("1.2.3.4"); got != want {
		t.Errorf("ipLink = %s", got)
	}
	if got := ipLink(`1.2.3.4"><b>`); strings.Contains(got, `"><b>`) {
		t.Errorf("ipLink must escape its input: %s", got)
	}
}

func TestSeenAtAcceptsSecondsAndMilliseconds(t *testing.T) {
	if seenAt(0) != "" {
		t.Error("zero is no time")
	}
	if seenAt(1700000000) != seenAt(1700000000000) {
		t.Error("seconds and milliseconds must format the same")
	}
}

func TestFormatIPLogLinksEveryAddress(t *testing.T) {
	got := formatIPLog(`[{"ip":"1.2.3.4","timestamp":1700000000},{"ip":"5.6.7.8","timestamp":0}]`)
	if !strings.Contains(got, ipLink("1.2.3.4")) || !strings.Contains(got, ipLink("5.6.7.8")) {
		t.Errorf("timestamped log: %s", got)
	}
	got = formatIPLog(`["9.9.9.9"]`)
	if got != ipLink("9.9.9.9") {
		t.Errorf("plain log: %s", got)
	}
	if got := formatIPLog(`<b>not json</b>`); strings.Contains(got, "<b>") {
		t.Errorf("raw text must be escaped: %s", got)
	}
}

func TestOnlineLabelSuffix(t *testing.T) {
	remarks := map[string]string{"a": "Germany", "b": ""}
	if got := onlineLabelSuffix([]string{"a", "b"}, remarks, ""); got != " · Germany, b" {
		t.Errorf("got %q", got)
	}
	if got := onlineLabelSuffix(nil, remarks, "Old"); got != " · Old" {
		t.Errorf("fallback: %q", got)
	}
	if got := onlineLabelSuffix(nil, remarks, ""); got != "" {
		t.Errorf("empty: %q", got)
	}
}

func TestHwidTitle(t *testing.T) {
	if got := hwidTitle("Pixel 8", "ua", "fp"); got != "Pixel 8" {
		t.Errorf("model first: %q", got)
	}
	if got := hwidTitle("", "MyUA", "fp"); got != "MyUA" {
		t.Errorf("then user agent: %q", got)
	}
	if got := hwidTitle("", "", "abc123"); got != "abc123" {
		t.Errorf("then fingerprint: %q", got)
	}
	long := hwidTitle(strings.Repeat("x", 100), "", "")
	if len([]rune(long)) != 41 {
		t.Errorf("long titles are cut to 40 characters plus an ellipsis, got %d", len([]rune(long)))
	}
}
