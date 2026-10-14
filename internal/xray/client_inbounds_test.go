package xray

import (
	"reflect"
	"testing"
)

const ciGrace = int64(20000)

func ciObs(email, ip, tag string, n int) ClientInboundObservation {
	return ClientInboundObservation{Email: email, IP: ip, Tag: tag, Count: n}
}

func ciLive(email string, ips ...string) map[string]map[string]struct{} {
	set := map[string]struct{}{}
	for _, ip := range ips {
		set[ip] = struct{}{}
	}
	return map[string]map[string]struct{}{email: set}
}

func ciRun(p *Process, now int64, online []string, obs []ClientInboundObservation, ips map[string]map[string]struct{}) {
	p.RefreshLocalOnline(online, nil, now, ciGrace)
	p.RefreshLocalClientInbounds(obs, ips, now, ciGrace)
}

func ciTags(p *Process, email string) []string { return p.GetLocalClientInbounds()[email] }

func TestClientInboundsProbeOnlyIsNotAttributed(t *testing.T) {
	p := newOnlineTestProcess()
	ips := ciLive("alice", "1.1.1.1")
	for m := int64(0); m < 20; m++ {
		ciRun(p, 1000+m*60000, []string{"alice"}, []ClientInboundObservation{
			ciObs("alice", "1.1.1.1", "in-1", 2), ciObs("alice", "1.1.1.1", "in-2", 2),
		}, ips)
	}
	if got := ciTags(p, "alice"); len(got) != 0 {
		t.Errorf("probe-only client must have no attribution, got %v", got)
	}
}

func TestClientInboundsRealUseBeatsProbes(t *testing.T) {
	p := newOnlineTestProcess()
	ips := ciLive("alice", "1.1.1.1")
	for m := int64(0); m < 20; m++ {
		ciRun(p, 1000+m*60000, []string{"alice"}, []ClientInboundObservation{
			ciObs("alice", "1.1.1.1", "in-1", 30), ciObs("alice", "1.1.1.1", "in-2", 2),
		}, ips)
	}
	if got := ciTags(p, "alice"); !reflect.DeepEqual(got, []string{"in-1"}) {
		t.Errorf("want [in-1], got %v", got)
	}
}

func TestClientInboundsLongLivedConnectionSticks(t *testing.T) {
	p := newOnlineTestProcess()
	ips := ciLive("alice", "1.1.1.1")
	ciRun(p, 1000, []string{"alice"}, []ClientInboundObservation{ciObs("alice", "1.1.1.1", "in-1", 12)}, ips)
	// Thirty quiet minutes: the score decays to ~0 but the IP is still live.
	ciRun(p, 1000+30*60000, []string{"alice"}, nil, ips)
	if got := ciTags(p, "alice"); !reflect.DeepEqual(got, []string{"in-1"}) {
		t.Errorf("sticky pair should remain, got %v", got)
	}
}

func TestClientInboundsDropsWhenIPLeaves(t *testing.T) {
	p := newOnlineTestProcess()
	ciRun(p, 1000, []string{"alice"}, []ClientInboundObservation{ciObs("alice", "1.1.1.1", "in-1", 12)}, ciLive("alice", "1.1.1.1"))
	ciRun(p, 61000, []string{"alice"}, nil, ciLive("alice", "9.9.9.9"))
	if got := ciTags(p, "alice"); len(got) != 0 {
		t.Errorf("pair of a departed IP must be hidden, got %v", got)
	}
}

func TestClientInboundsSwitchingInboundSupersedesSilentOne(t *testing.T) {
	p := newOnlineTestProcess()
	ips := ciLive("alice", "1.1.1.1")
	ciRun(p, 1000, []string{"alice"}, []ClientInboundObservation{ciObs("alice", "1.1.1.1", "in-1", 20)}, ips)
	ciRun(p, 1000+3*60000, []string{"alice"}, []ClientInboundObservation{ciObs("alice", "1.1.1.1", "in-2", 30)}, ips)
	if got := ciTags(p, "alice"); !reflect.DeepEqual(got, []string{"in-2"}) {
		t.Errorf("want [in-2] after the switch, got %v", got)
	}
}

func TestClientInboundsBothActiveStayVisible(t *testing.T) {
	p := newOnlineTestProcess()
	ciRun(p, 1000, []string{"alice"}, []ClientInboundObservation{
		ciObs("alice", "1.1.1.1", "in-1", 20), ciObs("alice", "1.1.1.1", "in-2", 20),
	}, ciLive("alice", "1.1.1.1"))
	if got := ciTags(p, "alice"); !reflect.DeepEqual(got, []string{"in-1", "in-2"}) {
		t.Errorf("want both inbounds, got %v", got)
	}
}

func TestClientInboundsTwoDevicesTwoInbounds(t *testing.T) {
	p := newOnlineTestProcess()
	ciRun(p, 1000, []string{"alice"}, []ClientInboundObservation{
		ciObs("alice", "1.1.1.1", "in-1", 20), ciObs("alice", "2.2.2.2", "in-2", 20),
	}, ciLive("alice", "1.1.1.1", "2.2.2.2"))
	if got := ciTags(p, "alice"); !reflect.DeepEqual(got, []string{"in-1", "in-2"}) {
		t.Errorf("each device keeps its inbound, got %v", got)
	}
}

func TestClientInboundsOfflineClientAndAgeOut(t *testing.T) {
	p := newOnlineTestProcess()
	ips := ciLive("alice", "1.1.1.1")
	ciRun(p, 1000, []string{"alice"}, []ClientInboundObservation{ciObs("alice", "1.1.1.1", "in-1", 20)}, ips)

	ciRun(p, 1000+ciGrace+1, nil, nil, nil)
	if got := ciTags(p, "alice"); len(got) != 0 {
		t.Errorf("offline client should be gone, got %v", got)
	}

	ciRun(p, 2000000, []string{"bob"}, []ClientInboundObservation{ciObs("bob", "3.3.3.3", "in-1", 20)}, ciLive("bob", "3.3.3.3"))
	ciRun(p, 2000000+clientInboundStickyMaxMs+1, []string{"bob"}, nil, ciLive("bob", "3.3.3.3"))
	if got := ciTags(p, "bob"); len(got) != 0 {
		t.Errorf("pair past the max age must expire, got %v", got)
	}
}

func TestClientInboundsUnknownIPsTreatedAsLive(t *testing.T) {
	p := newOnlineTestProcess()
	ciRun(p, 1000, []string{"alice"}, []ClientInboundObservation{ciObs("alice", "1.1.1.1", "in-1", 20)}, nil)
	if got := ciTags(p, "alice"); !reflect.DeepEqual(got, []string{"in-1"}) {
		t.Errorf("without IP info the pair should show, got %v", got)
	}
}

func TestClearLocalClientInbounds(t *testing.T) {
	p := newOnlineTestProcess()
	ciRun(p, 1000, []string{"alice"}, []ClientInboundObservation{ciObs("alice", "1.1.1.1", "in-1", 20)}, nil)
	p.ClearLocalClientInbounds()
	if got := p.GetLocalClientInbounds(); len(got) != 0 {
		t.Errorf("clear should empty the view, got %v", got)
	}
}

func TestNormalizeIP(t *testing.T) {
	if NormalizeIP("2001:0db8:0000:0000:0000:0000:0000:0001") != "2001:db8::1" || NormalizeIP("1.2.3.4") != "1.2.3.4" || NormalizeIP("junk") != "junk" {
		t.Error("NormalizeIP mismatch")
	}
}
