package tgbot

import "strings"

// onlineLabelSuffix lists every inbound a client is attributed to; with no tags
// it uses fallback (the legacy single-inbound remark, may be empty). The result
// is empty or starts with the separator, so it can follow the client's name.
func onlineLabelSuffix(tags []string, remarkByTag map[string]string, fallback string) string {
	names := make([]string, 0, len(tags))
	seen := make(map[string]struct{}, len(tags))
	for _, tag := range tags {
		name := strings.TrimSpace(remarkByTag[tag])
		if name == "" {
			name = tag
		}
		if name == "" {
			continue
		}
		if _, dup := seen[name]; dup {
			continue
		}
		seen[name] = struct{}{}
		names = append(names, name)
	}
	if len(names) == 0 {
		if fallback == "" {
			return ""
		}
		return " · " + fallback
	}
	return " · " + strings.Join(names, ", ")
}

// onlineButtonLabel is the client's name followed by its inbound label.
func onlineButtonLabel(email string, tags []string, remarkByTag map[string]string, fallback string) string {
	return email + onlineLabelSuffix(tags, remarkByTag, fallback)
}
