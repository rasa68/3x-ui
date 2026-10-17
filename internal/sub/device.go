package sub

import (
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/mhsanaei/3x-ui/v3/internal/web/service"
)

// Header and query aliases accepted for the device identity. The first
// non-empty value wins; headers are checked before query parameters.
var (
	hwidHeaders   = []string{"X-HWID", "X-Device-Id", "X-Device-ID", "X-Client-HWID", "HWID"}
	hwidQueries   = []string{"hwid", "device_id", "deviceId", "client_id", "uid"}
	modelHeaders  = []string{"X-Device-Model", "X-Model", "X-Device-Name", "Device-Model"}
	modelQueries  = []string{"model", "device", "device_model", "phone", "deviceName"}
	osHeaders     = []string{"X-Device-OS"}
	osVerHeaders  = []string{"X-Ver-OS"}
	maxDeviceMeta = 256
)

// resolvedDevice is what could be derived from a User-Agent alone.
type resolvedDevice struct {
	Model     string
	OS        string
	OsVersion string
	App       string
}

func firstHeader(c *gin.Context, names []string) string {
	for _, n := range names {
		if v := strings.TrimSpace(c.GetHeader(n)); v != "" {
			return v
		}
	}
	return ""
}

func firstQuery(c *gin.Context, names []string) string {
	for _, n := range names {
		if v := strings.TrimSpace(c.Query(n)); v != "" {
			return v
		}
	}
	return ""
}

func clip(s string) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) > maxDeviceMeta {
		return string(r[:maxDeviceMeta])
	}
	return string(r)
}

// hwidRequestFromContext builds the device identity of a subscription request.
// Explicit headers and query parameters win; whatever they leave blank (model,
// OS, OS version, app) is filled from the User-Agent.
func hwidRequestFromContext(c *gin.Context) service.HwidRequest {
	ua := c.GetHeader("User-Agent")
	dev := resolveDeviceFromUserAgent(ua)
	pick := func(explicit, resolved string) string {
		if explicit != "" {
			return clip(explicit)
		}
		return clip(resolved)
	}
	return service.HwidRequest{
		Hwid:        firstNonEmpty(firstHeader(c, hwidHeaders), firstQuery(c, hwidQueries)),
		UserAgent:   ua,
		DeviceOS:    pick(firstHeader(c, osHeaders), dev.OS),
		OsVersion:   pick(firstHeader(c, osVerHeaders), dev.OsVersion),
		DeviceModel: pick(firstNonEmpty(firstHeader(c, modelHeaders), firstQuery(c, modelQueries)), dev.Model),
		ClientApp:   clip(dev.App),
	}
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}

var (
	reAndroidVer = regexp.MustCompile(`(?i)Android[ /]([\d.]+)`)
	reAndroidDev = regexp.MustCompile(`(?i)Android[ /][\d.]+;\s*(?:[a-z]{2}-[a-z]{2};\s*)?([^;)]+?)(?:\s+Build/[^;)]*)?[;)]`)
	reAppleID    = regexp.MustCompile(`(iPhone|iPad|iPod)\d+,\d+`)
	reIOSVer     = regexp.MustCompile(`(?i)(?:iPhone OS|iPadOS|iOS)[ _/]([\d_.]+)`)
	reDarwin     = regexp.MustCompile(`Darwin/(\d+)`)
	reMacVer     = regexp.MustCompile(`Mac OS X ([\d_.]+)`)
	reWinNT      = regexp.MustCompile(`Windows NT ([\d.]+)`)
	reSamsung    = regexp.MustCompile(`\bSM-[A-Z]\d{3,4}[A-Z0-9]*`)
	rePixel      = regexp.MustCompile(`(?i)\bPixel[ _]?\d+[a-z]?(?:[ _](?:Pro(?:[ _]XL)?|XL|Fold))?`)
	genericApp   = regexp.MustCompile(`^([A-Za-z][A-Za-z0-9_.\-]*)/(\d[\w.\-]*)`)
)

// Ordered: more specific names first so "ClashMetaForAndroid" is not read as "Clash".
var appPatterns = []struct {
	re   *regexp.Regexp
	name string
}{
	{regexp.MustCompile(`(?i)v2rayNG/([\w.\-]+)`), "v2rayNG"},
	{regexp.MustCompile(`(?i)v2rayN/([\w.\-]+)`), "v2rayN"},
	{regexp.MustCompile(`(?i)V2Box/([\w.\-]+)`), "V2Box"},
	{regexp.MustCompile(`(?i)Streisand/([\w.\-]+)`), "Streisand"},
	{regexp.MustCompile(`(?i)Throne/([\w.\-]+)`), "Throne"},
	{regexp.MustCompile(`(?i)Happ(?:Proxy)?/([\w.\-]+)`), "Happ"},
	{regexp.MustCompile(`(?i)Exclave/([\w.\-]+)`), "Exclave"},
	{regexp.MustCompile(`(?i)Hiddify(?:Next)?/([\w.\-]+)`), "Hiddify"},
	{regexp.MustCompile(`(?i)Shadowrocket/([\w.\-]+)`), "Shadowrocket"},
	{regexp.MustCompile(`(?i)SFA/([\w.\-]+)`), "sing-box (SFA)"},
	{regexp.MustCompile(`(?i)SFM/([\w.\-]+)`), "sing-box (SFM)"},
	{regexp.MustCompile(`(?i)SFW/([\w.\-]+)`), "sing-box (SFW)"},
	{regexp.MustCompile(`(?i)sing-box/([\w.\-]+)`), "sing-box"},
	{regexp.MustCompile(`(?i)FlClash/([\w.\-]+)`), "FlClash"},
	{regexp.MustCompile(`(?i)ClashMetaForAndroid/([\w.\-]+)`), "Clash Meta for Android"},
	{regexp.MustCompile(`(?i)Clash[.\-]?Verge(?:[ \-]?Rev)?/([\w.\-]+)`), "Clash Verge"},
	{regexp.MustCompile(`(?i)Clash[.\-]?Meta/([\w.\-]+)`), "Clash Meta"},
	{regexp.MustCompile(`(?i)ClashForAndroid/([\w.\-]+)`), "Clash for Android"},
	{regexp.MustCompile(`(?i)Stash/([\w.\-]+)`), "Stash"},
	{regexp.MustCompile(`(?i)\bClash/([\w.\-]+)`), "Clash"},
	{regexp.MustCompile(`(?i)NekoBox(?:ForAndroid)?/([\w.\-]+)`), "NekoBox"},
	{regexp.MustCompile(`(?i)NekoRay/([\w.\-]+)`), "NekoRay"},
	{regexp.MustCompile(`(?i)Quantumult(?:%20| )?X/([\w.\-]+)`), "Quantumult X"},
	{regexp.MustCompile(`(?i)\bQuantumult/([\w.\-]+)`), "Quantumult"},
	{regexp.MustCompile(`(?i)Surge/([\w.\-]+)`), "Surge"},
	{regexp.MustCompile(`(?i)Loon/([\w.\-]+)`), "Loon"},
	{regexp.MustCompile(`(?i)Karing/([\w.\-]+)`), "Karing"},
	{regexp.MustCompile(`(?i)FoXray/([\w.\-]+)`), "FoXray"},
}

// Generic HTTP stacks and browsers are not VPN apps.
var notApps = map[string]bool{
	"mozilla": true, "curl": true, "wget": true, "okhttp": true, "dalvik": true,
	"cfnetwork": true, "darwin": true, "go-http-client": true, "python-requests": true,
	"python-urllib": true, "java": true, "apache-httpclient": true, "axios": true,
	"node-fetch": true, "postmanruntime": true, "libcurl": true, "chrome": true,
	"safari": true, "firefox": true, "opera": true, "edge": true, "applewebkit": true,
}

func resolveApp(ua string) string {
	for _, p := range appPatterns {
		if m := p.re.FindStringSubmatch(ua); m != nil {
			return p.name + " " + m[1]
		}
	}
	if m := genericApp.FindStringSubmatch(strings.TrimSpace(ua)); m != nil && !notApps[strings.ToLower(m[1])] {
		return m[1] + " " + m[2]
	}
	return ""
}

// resolveDeviceFromUserAgent derives device model, OS, OS version and the
// proxy client app from a User-Agent string, best effort. Any field it cannot
// determine stays empty.
func resolveDeviceFromUserAgent(ua string) resolvedDevice {
	ua = strings.TrimSpace(ua)
	if ua == "" {
		return resolvedDevice{}
	}
	dev := resolvedDevice{App: resolveApp(ua)}
	lower := strings.ToLower(ua)

	switch {
	case reAppleID.MatchString(ua) || strings.Contains(lower, "iphone") || strings.Contains(lower, "ipad") || strings.Contains(lower, "ipod"):
		resolveApple(ua, &dev)
	case strings.Contains(lower, "android"):
		resolveAndroid(ua, &dev)
	case strings.Contains(lower, "windows"):
		dev.OS = "Windows"
		dev.Model = "Windows PC"
		if m := reWinNT.FindStringSubmatch(ua); m != nil {
			dev.OsVersion = windowsVersion(m[1])
		}
	case strings.Contains(lower, "macintosh") || strings.Contains(lower, "mac os x") || strings.Contains(lower, "macos"):
		dev.OS = "macOS"
		dev.Model = macModel(lower)
		if m := reMacVer.FindStringSubmatch(ua); m != nil {
			dev.OsVersion = strings.ReplaceAll(m[1], "_", ".")
		} else if d := reDarwin.FindStringSubmatch(ua); d != nil {
			dev.OsVersion = macVersionFromDarwin(d[1])
		}
	case strings.Contains(lower, "linux") || strings.Contains(lower, "x11"):
		dev.OS = "Linux"
		dev.Model = "Linux PC"
	case reDarwin.MatchString(ua):
		// CFNetwork/Darwin only, no device hints: assume iOS like most clients do.
		dev.OS = "iOS"
		if d := reDarwin.FindStringSubmatch(ua); d != nil {
			dev.OsVersion = iosVersionFromDarwin(d[1])
		}
	}
	return dev
}

func resolveApple(ua string, dev *resolvedDevice) {
	lower := strings.ToLower(ua)
	id := reAppleID.FindString(ua)
	switch {
	case strings.HasPrefix(id, "iPad") || strings.Contains(lower, "ipad"):
		dev.OS = "iPadOS"
	default:
		dev.OS = "iOS"
	}
	switch {
	case id != "":
		if name, ok := appleDeviceNames[id]; ok {
			dev.Model = name
		} else {
			dev.Model = id
		}
	case strings.Contains(lower, "ipad"):
		dev.Model = "iPad"
	case strings.Contains(lower, "ipod"):
		dev.Model = "iPod touch"
	default:
		dev.Model = "iPhone"
	}
	if m := reIOSVer.FindStringSubmatch(ua); m != nil {
		dev.OsVersion = strings.ReplaceAll(m[1], "_", ".")
	} else if d := reDarwin.FindStringSubmatch(ua); d != nil {
		dev.OsVersion = iosVersionFromDarwin(d[1])
	}
}

func resolveAndroid(ua string, dev *resolvedDevice) {
	dev.OS = "Android"
	if m := reAndroidVer.FindStringSubmatch(ua); m != nil {
		dev.OsVersion = m[1]
	}
	raw := ""
	if m := reAndroidDev.FindStringSubmatch(ua); m != nil {
		raw = strings.TrimSpace(m[1])
	}
	if raw == "" {
		raw = firstMatch(ua, rePixel, reSamsung)
	}
	dev.Model = androidModelName(raw)
}

func firstMatch(s string, res ...*regexp.Regexp) string {
	for _, re := range res {
		if m := re.FindString(s); m != "" {
			return m
		}
	}
	return ""
}

func androidModelName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" || strings.EqualFold(raw, "K") || strings.EqualFold(raw, "wv") {
		return ""
	}
	if m := reSamsung.FindString(raw); m != "" {
		for prefix, name := range samsungModels {
			if strings.HasPrefix(m, prefix) {
				return "Samsung Galaxy " + name
			}
		}
		return "Samsung " + m
	}
	if m := rePixel.FindString(raw); m != "" {
		return "Google " + strings.ReplaceAll(m, "_", " ")
	}
	low := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(low, "redmi"), strings.HasPrefix(low, "poco"), strings.HasPrefix(low, "mi "), strings.HasPrefix(low, "xiaomi"):
		if strings.HasPrefix(low, "xiaomi") {
			return raw
		}
		return "Xiaomi " + raw
	case strings.HasPrefix(low, "oneplus"):
		return raw
	case strings.HasPrefix(low, "cph"):
		return "OPPO/OnePlus " + raw
	}
	return raw
}

func windowsVersion(nt string) string {
	switch nt {
	case "10.0":
		return "10/11"
	case "6.3":
		return "8.1"
	case "6.2":
		return "8"
	case "6.1":
		return "7"
	}
	return nt
}

func macModel(lower string) string {
	switch {
	case strings.Contains(lower, "arm64") || strings.Contains(lower, "apple silicon"):
		return "Mac (Apple Silicon)"
	case strings.Contains(lower, "intel") || strings.Contains(lower, "x86_64"):
		return "Mac (Intel)"
	}
	return "Mac"
}

// Darwin kernel major -> marketing version.
func iosVersionFromDarwin(major string) string {
	n, err := strconv.Atoi(major)
	if err != nil {
		return ""
	}
	if n >= 25 {
		return strconv.Itoa(n + 1) // Darwin 25 -> iOS 26, 26 -> 27 ...
	}
	if v, ok := map[int]string{24: "18", 23: "17", 22: "16", 21: "15", 20: "14", 19: "13", 18: "12", 17: "11"}[n]; ok {
		return v
	}
	return ""
}

func macVersionFromDarwin(major string) string {
	n, err := strconv.Atoi(major)
	if err != nil {
		return ""
	}
	if n >= 25 {
		return strconv.Itoa(n + 1)
	}
	if v, ok := map[int]string{24: "15", 23: "14", 22: "13", 21: "12", 20: "11"}[n]; ok {
		return v
	}
	return ""
}

var appleDeviceNames = map[string]string{
	"iPhone17,1": "iPhone 16 Pro", "iPhone17,2": "iPhone 16 Pro Max", "iPhone17,3": "iPhone 16",
	"iPhone17,4": "iPhone 16 Plus", "iPhone17,5": "iPhone 16e",
	"iPhone16,1": "iPhone 15 Pro", "iPhone16,2": "iPhone 15 Pro Max",
	"iPhone15,4": "iPhone 15", "iPhone15,5": "iPhone 15 Plus",
	"iPhone15,2": "iPhone 14 Pro", "iPhone15,3": "iPhone 14 Pro Max",
	"iPhone14,7": "iPhone 14", "iPhone14,8": "iPhone 14 Plus",
	"iPhone14,2": "iPhone 13 Pro", "iPhone14,3": "iPhone 13 Pro Max",
	"iPhone14,5": "iPhone 13", "iPhone14,4": "iPhone 13 mini", "iPhone14,6": "iPhone SE (3rd gen)",
	"iPhone13,1": "iPhone 12 mini", "iPhone13,2": "iPhone 12", "iPhone13,3": "iPhone 12 Pro", "iPhone13,4": "iPhone 12 Pro Max",
	"iPhone12,1": "iPhone 11", "iPhone12,3": "iPhone 11 Pro", "iPhone12,5": "iPhone 11 Pro Max", "iPhone12,8": "iPhone SE (2nd gen)",
	"iPhone11,2": "iPhone XS", "iPhone11,4": "iPhone XS Max", "iPhone11,6": "iPhone XS Max", "iPhone11,8": "iPhone XR",
	"iPhone10,3": "iPhone X", "iPhone10,6": "iPhone X",
	"iPhone10,1": "iPhone 8", "iPhone10,4": "iPhone 8", "iPhone10,2": "iPhone 8 Plus", "iPhone10,5": "iPhone 8 Plus",
	"iPad11,6": "iPad (8th gen)", "iPad11,7": "iPad (8th gen)", "iPad12,1": "iPad (9th gen)", "iPad12,2": "iPad (9th gen)",
	"iPad13,18": "iPad (10th gen)", "iPad13,19": "iPad (10th gen)",
	"iPad13,1": "iPad Air (4th gen)", "iPad13,2": "iPad Air (4th gen)",
	"iPad13,16": "iPad Air (5th gen)", "iPad13,17": "iPad Air (5th gen)",
	"iPad14,1": "iPad mini (6th gen)", "iPad14,2": "iPad mini (6th gen)",
	"iPad13,4": "iPad Pro 11-inch (3rd gen)", "iPad13,5": "iPad Pro 11-inch (3rd gen)",
	"iPad13,6": "iPad Pro 11-inch (3rd gen)", "iPad13,7": "iPad Pro 11-inch (3rd gen)",
	"iPad13,8": "iPad Pro 12.9-inch (5th gen)", "iPad13,9": "iPad Pro 12.9-inch (5th gen)",
	"iPad13,10": "iPad Pro 12.9-inch (5th gen)", "iPad13,11": "iPad Pro 12.9-inch (5th gen)",
	"iPad14,3": "iPad Pro 11-inch (4th gen)", "iPad14,4": "iPad Pro 11-inch (4th gen)",
	"iPad14,5": "iPad Pro 12.9-inch (6th gen)", "iPad14,6": "iPad Pro 12.9-inch (6th gen)",
	"iPad14,8": "iPad Air 11-inch (M2)", "iPad14,9": "iPad Air 11-inch (M2)",
	"iPad14,10": "iPad Air 13-inch (M2)", "iPad14,11": "iPad Air 13-inch (M2)",
	"iPad16,3": "iPad Pro 11-inch (M4)", "iPad16,4": "iPad Pro 11-inch (M4)",
	"iPad16,5": "iPad Pro 13-inch (M4)", "iPad16,6": "iPad Pro 13-inch (M4)",
}

// Samsung model-number prefixes (region/variant suffix ignored).
var samsungModels = map[string]string{
	"SM-S938": "S25 Ultra", "SM-S936": "S25+", "SM-S931": "S25",
	"SM-S928": "S24 Ultra", "SM-S926": "S24+", "SM-S921": "S24",
	"SM-S918": "S23 Ultra", "SM-S916": "S23+", "SM-S911": "S23",
	"SM-S908": "S22 Ultra", "SM-S906": "S22+", "SM-S901": "S22",
	"SM-G998": "S21 Ultra", "SM-G996": "S21+", "SM-G991": "S21",
	"SM-G988": "S20 Ultra", "SM-G986": "S20+", "SM-G985": "S20+", "SM-G981": "S20", "SM-G980": "S20",
	"SM-N986": "Note20 Ultra", "SM-N985": "Note20 Ultra", "SM-N981": "Note20", "SM-N980": "Note20",
	"SM-A556": "A55 5G", "SM-A546": "A54 5G", "SM-A536": "A53 5G", "SM-A528": "A52s 5G",
	"SM-A356": "A35 5G", "SM-A346": "A34 5G", "SM-A336": "A33 5G", "SM-A256": "A25 5G", "SM-A156": "A15 5G",
	"SM-A057": "A05s", "SM-A055": "A05",
	"SM-F956": "Z Fold6", "SM-F946": "Z Fold5", "SM-F936": "Z Fold4", "SM-F926": "Z Fold3",
	"SM-F741": "Z Flip6", "SM-F731": "Z Flip5", "SM-F721": "Z Flip4", "SM-F711": "Z Flip3",
}
