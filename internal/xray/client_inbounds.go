package xray

import (
	"math"
	"net"
	"sort"
)

// Tuned so a balancer probe (~2 connections/min, steady score ~4) never confirms,
// while real browsing (tens of connections/min) does within seconds.
const (
	clientInboundHalfLifeMs       int64   = 60 * 1000
	clientInboundConfirmScore     float64 = 8
	clientInboundSupersedeMs      int64   = 2 * 60 * 1000
	clientInboundStickyMaxMs      int64   = 2 * 60 * 60 * 1000
	clientInboundUnconfirmedTTLMs int64   = 10 * 60 * 1000
)

// ClientInboundObservation is the number of connections one client opened from
// one IP on one inbound tag since the previous poll (from the access log).
type ClientInboundObservation struct {
	Email string
	IP    string
	Tag   string
	Count int
}

type clientInboundKey struct{ email, ip, tag string }

type clientInboundState struct {
	score     float64
	scoreAt   int64
	lastLog   int64
	confirmed bool
}

// NormalizeIP returns the canonical text form of an IP so log and core values compare equal.
func NormalizeIP(s string) string {
	if ip := net.ParseIP(s); ip != nil {
		return ip.String()
	}
	return s
}

func decayScore(score float64, from, to int64) float64 {
	if to <= from {
		return score
	}
	return score * math.Exp2(-float64(to-from)/float64(clientInboundHalfLifeMs))
}

// RefreshLocalClientInbounds folds this poll's access-log observations into a
// decaying per-(client, IP, inbound) score and rebuilds the visible view.
// A pair is confirmed once its score reaches clientInboundConfirmScore (probes
// and pings stay below it) and then sticks, so a long-lived low-volume
// connection stays attributed, until its IP leaves onlineIPs, the client goes
// offline, the pair ages out, or another inbound of the same IP is confirmed
// while this one has been silent. onlineIPs (email -> live IPs from the core)
// may be nil; an email without entries has its IPs treated as live.
func (p *Process) RefreshLocalClientInbounds(obs []ClientInboundObservation, onlineIPs map[string]map[string]struct{}, now, graceMs int64) {
	p.onlineMu.Lock()
	defer p.onlineMu.Unlock()
	if p.clientInboundStates == nil {
		p.clientInboundStates = make(map[clientInboundKey]*clientInboundState, len(obs))
	}
	for _, o := range obs {
		if o.Email == "" || o.Tag == "" || o.Count <= 0 {
			continue
		}
		k := clientInboundKey{o.Email, NormalizeIP(o.IP), o.Tag}
		st := p.clientInboundStates[k]
		if st == nil {
			st = &clientInboundState{scoreAt: now}
			p.clientInboundStates[k] = st
		}
		st.score = decayScore(st.score, st.scoreAt, now) + float64(o.Count)
		st.scoreAt, st.lastLog = now, now
		if st.score >= clientInboundConfirmScore {
			st.confirmed = true
		}
	}

	online := make(map[string]struct{}, len(p.onlineClients))
	for _, email := range p.onlineClients {
		online[email] = struct{}{}
	}

	type group struct{ email, ip string }
	strong := make(map[group]struct{})
	current := make(map[clientInboundKey]float64, len(p.clientInboundStates))
	for k, st := range p.clientInboundStates {
		age := now - st.lastLog
		_, isOnline := online[k.email]
		if age >= clientInboundStickyMaxMs || (!st.confirmed && age >= clientInboundUnconfirmedTTLMs) || (!isOnline && age >= graceMs) {
			delete(p.clientInboundStates, k)
			continue
		}
		c := decayScore(st.score, st.scoreAt, now)
		current[k] = c
		if c >= clientInboundConfirmScore {
			st.confirmed = true
			strong[group{k.email, k.ip}] = struct{}{}
		}
	}

	sets := make(map[string]map[string]struct{})
	for k, st := range p.clientInboundStates {
		if _, ok := online[k.email]; !ok || !st.confirmed {
			continue
		}
		if ips := onlineIPs[k.email]; len(ips) > 0 && k.ip != "" {
			if _, live := ips[k.ip]; !live {
				continue
			}
		}
		if current[k] < clientInboundConfirmScore {
			_, other := strong[group{k.email, k.ip}]
			if other && now-st.lastLog >= clientInboundSupersedeMs {
				continue
			}
		}
		if sets[k.email] == nil {
			sets[k.email] = make(map[string]struct{})
		}
		sets[k.email][k.tag] = struct{}{}
	}
	view := make(map[string][]string, len(sets))
	for email, tags := range sets {
		list := make([]string, 0, len(tags))
		for tag := range tags {
			list = append(list, tag)
		}
		sort.Strings(list)
		view[email] = list
	}
	p.localClientInboundsView = view
}

// ClearLocalClientInbounds drops all attribution, e.g. when the access log is turned off.
func (p *Process) ClearLocalClientInbounds() {
	p.onlineMu.Lock()
	defer p.onlineMu.Unlock()
	p.clientInboundStates = nil
	p.localClientInboundsView = nil
}

// GetLocalClientInbounds returns a copy of email -> attributed inbound tags;
// a missing email means no attribution, so callers fall back.
func (p *Process) GetLocalClientInbounds() map[string][]string {
	p.onlineMu.RLock()
	defer p.onlineMu.RUnlock()
	out := make(map[string][]string, len(p.localClientInboundsView))
	for email, tags := range p.localClientInboundsView {
		out[email] = append([]string(nil), tags...)
	}
	return out
}
