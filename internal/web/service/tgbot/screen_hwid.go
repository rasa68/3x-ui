package tgbot

import (
	"html"
	"strconv"
	"strings"

	"github.com/mymmrac/telego"
	tu "github.com/mymmrac/telego/telegoutil"
)

const hwidDeletePrefix = "🗑 "

const (
	maxHwidDeviceRows   = 20
	hwidDeleteBtnPerRow = 5
)

// hwidTitle names one registered device by what is known about it.
func hwidTitle(model, userAgent, fingerprint string) string {
	for _, v := range []string{model, userAgent, fingerprint} {
		if v = strings.TrimSpace(v); v != "" {
			if r := []rune(v); len(r) > 40 {
				v = string(r[:40]) + "…"
			}
			return v
		}
	}
	return ""
}

// screenClientHwids draws the HWID limit and the registered devices of a client,
// with one delete button per device.
func (t *Tgbot) screenClientHwids(chatID int64, email string) {
	record, err := t.clientService.GetRecordByEmail(nil, email)
	if err != nil || record == nil {
		t.sendNotice(chatID, t.I18nBot("tgbot.noResult"))
		return
	}
	devices, err := t.clientService.ListClientHwids(email)
	if err != nil {
		t.sendNotice(chatID, t.I18nBot("tgbot.answers.errorOperation"))
		return
	}
	registered := 0
	for _, d := range devices {
		if !d.Estimated {
			registered++
		}
	}
	limit := "∞"
	if record.LimitHwid > 0 {
		limit = strconv.Itoa(record.LimitHwid)
	}
	body := t.clientScreenBody(email, "")
	body += t.I18nBot("tgbot.messages.hwidSummary", "Limit=="+limit, "Count=="+strconv.Itoa(registered))
	if len(devices) == 0 {
		body += t.I18nBot("tgbot.noHwidDevices")
	}
	var deleteButtons []telego.InlineKeyboardButton
	for i, d := range devices {
		if i == maxHwidDeviceRows {
			break
		}
		n := strconv.Itoa(i + 1)
		parts := []string{html.EscapeString(hwidTitle(d.DeviceModel, d.UserAgent, d.Fingerprint))}
		if osName := strings.TrimSpace(d.DeviceOS + " " + d.OsVersion); osName != "" {
			parts = append(parts, html.EscapeString(osName))
		}
		if d.ClientApp != "" {
			parts = append(parts, html.EscapeString(d.ClientApp))
		}
		if seen := seenAt(d.LastSeen); seen != "" {
			parts = append(parts, html.EscapeString(seen))
		}
		line := n + ". " + strings.Join(parts, " · ")
		if d.Estimated {
			line += " (" + html.EscapeString(t.I18nBot("tgbot.messages.hwidEstimated")) + ")"
		}
		body += line + "\n"
		deleteButtons = append(deleteButtons, t.btn2(hwidDeletePrefix+n, t.encodeQuery("hwid_del "+email+" "+strconv.Itoa(d.Id))))
	}
	var rows [][]telego.InlineKeyboardButton
	for len(deleteButtons) > 0 {
		n := min(hwidDeleteBtnPerRow, len(deleteButtons))
		rows = append(rows, tu.InlineKeyboardRow(deleteButtons[:n]...))
		deleteButtons = deleteButtons[n:]
	}
	rows = append(rows,
		tu.InlineKeyboardRow(t.btn("tgbot.buttons.refresh", t.encodeQuery("hwid_devices "+email))),
		tu.InlineKeyboardRow(t.btn("tgbot.buttons.clientCard", t.encodeQuery("client_get_usage "+email))),
	)
	t.renderScreen(chatID, t.newScreen("client", body, rows...))
}

// hwidLimitPicker is the preset keyboard for the HWID limit, same shape as the IP limit's.
func (t *Tgbot) hwidLimitPicker(email string) [][]telego.InlineKeyboardButton {
	preset := func(n int) telego.InlineKeyboardButton {
		return t.btn2(strconv.Itoa(n), t.encodeQuery("hwid_limit_c "+email+" "+strconv.Itoa(n)))
	}
	return [][]telego.InlineKeyboardButton{
		tu.InlineKeyboardRow(t.btn("tgbot.buttons.cancel", t.encodeQuery("client_cancel "+email))),
		tu.InlineKeyboardRow(
			t.btn("tgbot.unlimited", t.encodeQuery("hwid_limit_c "+email+" 0")),
			t.btn("tgbot.buttons.custom", t.encodeQuery("hwid_limit_in "+email+" 0")),
		),
		tu.InlineKeyboardRow(preset(1), preset(2)),
		tu.InlineKeyboardRow(preset(3), preset(4)),
		tu.InlineKeyboardRow(preset(5), preset(6), preset(7)),
		tu.InlineKeyboardRow(preset(8), preset(9), preset(10)),
	}
}
