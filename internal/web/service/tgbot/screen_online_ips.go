package tgbot

import (
	"encoding/base64"
	"html"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/xray"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const (
	// onlinePayloadPrefix marks a /start payload that opens a client's online IPs.
	// "." is not allowed in a Telegram payload, so the marker is a letter and "_".
	onlinePayloadPrefix = "o_"
	// maxStartPayload is Telegram's limit for a /start payload.
	maxStartPayload = 64
	maxOnlineIPRows = 50
	checkHostInfo   = "https://check-host.net/ip-info?host="
)

// encodeOnlinePayload builds the /start payload of a client's online IPs link;
// ok is false when the email is too long to fit one.
func encodeOnlinePayload(email string) (string, bool) {
	payload := onlinePayloadPrefix + base64.RawURLEncoding.EncodeToString([]byte(email))
	return payload, len(payload) <= maxStartPayload
}

func decodeOnlinePayload(payload string) (string, bool) {
	payload = strings.TrimSpace(payload)
	if !strings.HasPrefix(payload, onlinePayloadPrefix) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(payload, onlinePayloadPrefix))
	if err != nil || len(raw) == 0 {
		return "", false
	}
	return string(raw), true
}

// onlineClientLink is the deep link that sends the admin to the client's online
// IPs; "" when the bot has no username yet or the email does not fit a payload.
func onlineClientLink(email string) string {
	username := botUsername()
	if username == "" {
		return ""
	}
	payload, ok := encodeOnlinePayload(email)
	if !ok {
		return ""
	}
	return "https://t.me/" + username + "?start=" + payload
}

// checkHostURL is the check-host.net page describing one IP address.
func checkHostURL(ip string) string {
	return checkHostInfo + url.QueryEscape(ip)
}

// ipLink renders an IP as a link to its check-host.net page.
func ipLink(ip string) string {
	return `<a href="` + html.EscapeString(checkHostURL(ip)) + `">` + html.EscapeString(ip) + `</a>`
}

// seenAt formats a last-seen stamp that may be in seconds or milliseconds.
func seenAt(stamp int64) string {
	if stamp <= 0 {
		return ""
	}
	if stamp > 1e12 {
		stamp /= 1000
	}
	return time.Unix(stamp, 0).Format("2006-01-02 15:04:05")
}

// onlineIPsOf returns the live source IPs of one client, newest first.
func (t *Tgbot) onlineIPsOf(email string) []xray.OnlineIP {
	users, ok, err := t.xrayService.GetOnlineUsers()
	if err != nil || !ok {
		return nil
	}
	for _, u := range users {
		if u.Email != email {
			continue
		}
		ips := append([]xray.OnlineIP(nil), u.IPs...)
		sort.SliceStable(ips, func(i, j int) bool { return ips[i].LastSeen > ips[j].LastSeen })
		return ips
	}
	return nil
}

// screenClientOnlineIPs draws the IPs a client is connected from right now,
// each linking to its check-host.net page. Its single back button returns to the
// online clients screen it was opened from.
func (t *Tgbot) screenClientOnlineIPs(chatID int64, email string, delivery ...screenDelivery) {
	body := t.clientScreenBody(email, "")
	if process := xrayProcessOrNil(); process != nil {
		if tags := process.GetLocalClientInbounds()[email]; len(tags) > 0 {
			remarkByTag := map[string]string{}
			if inbounds, err := t.inboundService.GetAllInbounds(); err == nil {
				for _, ib := range inbounds {
					remarkByTag[ib.Tag] = ib.Remark
				}
			}
			body += html.EscapeString(strings.TrimPrefix(onlineLabelSuffix(tags, remarkByTag, ""), " · ")) + "\n\n"
		}
	}
	ips := t.onlineIPsOf(email)
	if len(ips) == 0 {
		body += t.I18nBot("tgbot.noOnlineIps")
	}
	for i, entry := range ips {
		if i == maxOnlineIPRows {
			break
		}
		line := "• " + ipLink(entry.IP)
		if seen := seenAt(entry.LastSeen); seen != "" {
			line += " (" + html.EscapeString(seen) + ")"
		}
		body += line + "\n"
	}
	body += t.I18nBot("tgbot.messages.refreshedOn", "Time=="+time.Now().Format("2006-01-02 15:04:05"))
	rows := [][]telego.InlineKeyboardButton{
		tu.InlineKeyboardRow(t.btn("tgbot.buttons.refresh", t.encodeQuery("onl_ips "+email))),
		tu.InlineKeyboardRow(t.btn("tgbot.buttons.clientCard", t.encodeQuery("client_get_usage "+email))),
		tu.InlineKeyboardRow(t.btn("tgbot.buttons.back", cbOnlines)),
	}
	sc := t.newScreen("client", body, rows...)
	// The only way up is the online clients screen, not the main menu.
	sc.rows = stripWayOut(sc.rows)
	t.renderScreen(chatID, sc, delivery...)
}
