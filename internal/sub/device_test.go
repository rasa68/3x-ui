package sub

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestResolveDeviceFromUserAgent(t *testing.T) {
	cases := []struct {
		name string
		ua   string
		want resolvedDevice
	}{
		{"iPhone via identifier and Darwin", "Happ/2.1.0 CFNetwork/1485 Darwin/23.1.0 iPhone15,2",
			resolvedDevice{Model: "iPhone 14 Pro", OS: "iOS", OsVersion: "17", App: "Happ 2.1.0"}},
		{"explicit iOS version wins", "Quantumult%20X/1.4.1 (iPhone14,5; iOS 16.6)",
			resolvedDevice{Model: "iPhone 13", OS: "iOS", OsVersion: "16.6", App: "Quantumult X 1.4.1"}},
		{"iPad", "Shadowrocket/2.2.40 iPad13,1 iPadOS 16.1",
			resolvedDevice{Model: "iPad Air (4th gen)", OS: "iPadOS", OsVersion: "16.1", App: "Shadowrocket 2.2.40"}},
		{"unknown apple identifier kept", "Streisand/1.5 iPhone99,9",
			resolvedDevice{Model: "iPhone99,9", OS: "iOS", App: "Streisand 1.5"}},
		{"darwin only is treated as iOS", "Streisand/1.5 CFNetwork/1568.100.1 Darwin/24.0.0",
			resolvedDevice{OS: "iOS", OsVersion: "18", App: "Streisand 1.5"}},
		{"samsung model number", "v2rayNG/1.8.5 Mozilla/5.0 (Linux; Android 14; SM-S928B Build/UP1A.231005.007)",
			resolvedDevice{Model: "Samsung Galaxy S24 Ultra", OS: "Android", OsVersion: "14", App: "v2rayNG 1.8.5"}},
		{"pixel without android version", "Hiddify/2.0.5 (android; Pixel 8 Pro)",
			resolvedDevice{Model: "Google Pixel 8 Pro", OS: "Android", App: "Hiddify 2.0.5"}},
		{"redmi", "Mozilla/5.0 (Linux; Android 13; Redmi Note 12 Build/TKQ1.221114.001)",
			resolvedDevice{Model: "Xiaomi Redmi Note 12", OS: "Android", OsVersion: "13"}},
		{"windows", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) Clash-verge/1.7.7",
			resolvedDevice{Model: "Windows PC", OS: "Windows", OsVersion: "10/11", App: "Clash Verge 1.7.7"}},
		{"intel mac", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/605.1.15",
			resolvedDevice{Model: "Mac (Intel)", OS: "macOS", OsVersion: "10.15.7"}},
		{"linux", "Mozilla/5.0 (X11; Linux x86_64)",
			resolvedDevice{Model: "Linux PC", OS: "Linux"}},
		{"curl is not an app", "curl/8.4.0", resolvedDevice{}},
		{"generic app fallback", "MyVPN/3.2.1 (custom)", resolvedDevice{App: "MyVPN 3.2.1"}},
		{"flclash is not plain clash", "FlClash/0.8.60", resolvedDevice{App: "FlClash 0.8.60"}},
		{"empty", "", resolvedDevice{}},
	}
	for _, c := range cases {
		if got := resolveDeviceFromUserAgent(c.ua); got != c.want {
			t.Errorf("%s:\n got  %+v\n want %+v", c.name, got, c.want)
		}
	}
}

func TestIosVersionFromDarwin(t *testing.T) {
	for in, want := range map[string]string{"24": "18", "23": "17", "22": "16", "25": "26", "26": "27", "x": "", "5": ""} {
		if got := iosVersionFromDarwin(in); got != want {
			t.Errorf("iosVersionFromDarwin(%q) = %q, want %q", in, got, want)
		}
	}
}

func newHwidTestContext(rawURL string, headers map[string]string) *gin.Context {
	gin.SetMode(gin.TestMode)
	req := httptest.NewRequest(http.MethodGet, rawURL, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	return c
}

func TestHwidRequestFromContext(t *testing.T) {
	t.Run("query aliases", func(t *testing.T) {
		got := hwidRequestFromContext(newHwidTestContext("/sub/x?deviceId=abc12345&phone=MyPhone", nil))
		if got.Hwid != "abc12345" || got.DeviceModel != "MyPhone" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("header alias beats query", func(t *testing.T) {
		got := hwidRequestFromContext(newHwidTestContext("/sub/x?hwid=fromquery1", map[string]string{"X-Device-ID": "fromheader1", "Device-Model": "HdrModel"}))
		if got.Hwid != "fromheader1" || got.DeviceModel != "HdrModel" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("explicit model beats user agent", func(t *testing.T) {
		got := hwidRequestFromContext(newHwidTestContext("/sub/x?model=Custom", map[string]string{
			"User-Agent": "v2rayNG/1.8.5 Mozilla/5.0 (Linux; Android 14; SM-S928B Build/UP1A)",
		}))
		if got.DeviceModel != "Custom" || got.DeviceOS != "Android" || got.OsVersion != "14" || got.ClientApp != "v2rayNG 1.8.5" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("user agent fills blanks", func(t *testing.T) {
		got := hwidRequestFromContext(newHwidTestContext("/sub/x", map[string]string{
			"X-HWID": "hwid-123456", "User-Agent": "Happ/2.1.0 CFNetwork/1485 Darwin/23.1.0 iPhone15,2",
		}))
		if got.DeviceModel != "iPhone 14 Pro" || got.DeviceOS != "iOS" || got.OsVersion != "17" {
			t.Errorf("got %+v", got)
		}
	})
	t.Run("explicit os headers kept", func(t *testing.T) {
		got := hwidRequestFromContext(newHwidTestContext("/sub/x", map[string]string{"X-Device-OS": "android", "X-Ver-OS": "15"}))
		if got.DeviceOS != "android" || got.OsVersion != "15" {
			t.Errorf("got %+v", got)
		}
	})
}
