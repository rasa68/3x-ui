package service

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseAccessLogClientInbound(t *testing.T) {
	cases := []struct {
		name      string
		line      string
		wantOK    bool
		wantEmail string
		wantTag   string
		wantIP    string
	}{
		{"direct", "2026/10/12 10:00:01.123456 from 1.2.3.4:5678 accepted tcp:example.com:443 [inbound-443 >> direct] email: alice", true, "alice", "inbound-443", "1.2.3.4"},
		{"routed arrow", "2026/10/12 10:00:01.123456 from 1.2.3.4:5678 accepted udp:1.1.1.1:53 [in-8443 -> proxy] email: bob@example.com", true, "bob@example.com", "in-8443", "1.2.3.4"},
		{"tcp prefix and ipv6", "2026/10/12 10:00:01.123456 from tcp:[2001:db8::1]:443 accepted tcp:a.com:443 [in-1 >> direct] email: carol", true, "carol", "in-1", "2001:db8::1"},
		{"api call", "2026/10/12 10:00:01 from 127.0.0.1:1 accepted tcp:127.0.0.1:62789 [api -> api]", false, "", "", ""},
		{"no email", "2026/10/12 10:00:01 from 1.2.3.4:5 accepted tcp:a.com:80 [in-1 >> direct]", false, "", "", ""},
		{"garbage", "not a log line", false, "", "", ""},
	}
	for _, c := range cases {
		email, tag, ip, _, ok := parseAccessLogClientInbound(c.line)
		if ok != c.wantOK || email != c.wantEmail || tag != c.wantTag || ip != c.wantIP {
			t.Errorf("%s: got (%q,%q,%q,%v), want (%q,%q,%q,%v)", c.name, email, tag, ip, ok, c.wantEmail, c.wantTag, c.wantIP, c.wantOK)
		}
	}
}

func pollPairs(tl *accessLogTailer) map[string][]string {
	obs, _ := tl.poll()
	if obs == nil {
		return nil
	}
	out := map[string][]string{}
	for _, o := range obs {
		out[o.Email] = append(out[o.Email], o.Tag)
	}
	return out
}

func TestAccessLogTailerCountsConnectionsPerIPAndInbound(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	ts := time.Now().Format("2006/01/02 15:04:05.000000")
	var b strings.Builder
	for range 3 {
		b.WriteString(ts + " from 1.1.1.1:1000 accepted tcp:a.com:443 [in-1 >> direct] email: alice\n")
	}
	b.WriteString(ts + " from 2.2.2.2:2000 accepted tcp:a.com:443 [in-1 >> direct] email: alice\n")
	if err := os.WriteFile(path, []byte(b.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	tail := &accessLogTailer{path: path, pathChecked: time.Now()}
	obs, enabled := tail.poll()
	if !enabled || len(obs) != 2 {
		t.Fatalf("want 2 observations, got %v (enabled=%v)", obs, enabled)
	}
	for _, o := range obs {
		want := map[string]int{"1.1.1.1": 3, "2.2.2.2": 1}[o.IP]
		if o.Email != "alice" || o.Tag != "in-1" || o.Count != want {
			t.Errorf("unexpected observation %+v", o)
		}
	}
}

func TestAccessLogTailerReadsOnlyNewLines(t *testing.T) {
	path := filepath.Join(t.TempDir(), "access.log")
	line := func(email, tag string) string {
		return time.Now().Format("2006/01/02 15:04:05.000000") + " from 1.2.3.4:5 accepted tcp:a.com:443 [" + tag + " >> direct] email: " + email + "\n"
	}
	if err := os.WriteFile(path, []byte(line("alice", "in-1")), 0o600); err != nil {
		t.Fatal(err)
	}
	tail := &accessLogTailer{path: path, pathChecked: time.Now()}

	if got := pollPairs(tail); len(got["alice"]) != 1 || got["alice"][0] != "in-1" {
		t.Fatalf("seed poll: got %v", got)
	}
	if got := pollPairs(tail); got != nil {
		t.Fatalf("nothing new, got %v", got)
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	// A complete line plus a half-written one: only the complete one counts now.
	half := line("carol", "in-3")
	if _, err := f.WriteString(line("bob", "in-2") + half[:20]); err != nil {
		t.Fatal(err)
	}
	got := pollPairs(tail)
	if len(got) != 1 || got["bob"][0] != "in-2" {
		t.Fatalf("append poll: got %v", got)
	}
	if _, err := f.WriteString(half[20:]); err != nil {
		t.Fatal(err)
	}
	if got := pollPairs(tail); got["carol"][0] != "in-3" {
		t.Fatalf("completed line should be picked up, got %v", got)
	}

	// Truncation (clear_logs_job) restarts from the top.
	if err := os.WriteFile(path, []byte(line("dave", "in-4")), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := pollPairs(tail); got["dave"][0] != "in-4" {
		t.Fatalf("after truncation: got %v", got)
	}
}
