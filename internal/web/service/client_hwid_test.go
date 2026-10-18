package service

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/dbtest"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
)

func initClientHwidTestDB(t *testing.T) {
	t.Helper()
	dbDir := t.TempDir()
	t.Setenv("XUI_DB_FOLDER", dbDir)
	dbtest.InitDB(t, filepath.Join(dbDir, "x-ui.db"))
}

func seedHwidClient(t *testing.T, limit int) *model.ClientRecord {
	t.Helper()
	rec := &model.ClientRecord{
		Email:     "hwid@example.com",
		SubID:     "sub-hwid",
		UUID:      "11111111-2222-4333-8444-555555555555",
		Enable:    true,
		LimitHwid: limit,
	}
	if err := database.GetDB().Create(rec).Error; err != nil {
		t.Fatalf("seed client: %v", err)
	}
	return rec
}

func TestClientHwidGate(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}

	seedHwidClient(t, 0)
	res, err := svc.EnforceHwidForSubID("sub-hwid", HwidRequest{})
	if err != nil {
		t.Fatalf("no-limit gate: %v", err)
	}
	if !res.Allowed || res.Active {
		t.Fatalf("no limit should allow missing HWID without active headers: %+v", res)
	}

	for _, ua := range []string{"Happ/1.0", "Happ/2.0"} {
		res, err = svc.EnforceHwidForSubID("sub-hwid", HwidRequest{Hwid: "device-one", UserAgent: ua})
		if err != nil {
			t.Fatalf("no-limit gate with HWID: %v", err)
		}
		if res != (HwidGateResult{Allowed: true}) {
			t.Fatalf("no limit should allow HWID without active headers: %+v", res)
		}
	}
	list, err := svc.ListClientHwids("hwid@example.com")
	if err != nil {
		t.Fatalf("list HWIDs: %v", err)
	}
	if len(list) != 1 || list[0].UserAgent != "Happ/2.0" {
		t.Fatalf("no limit should still track one device with fresh metadata, got %+v", list)
	}
}

func TestClientHwidGateRegistersAndBlocks(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}
	rec := seedHwidClient(t, 2)

	res, err := svc.EnforceHwidForSubID(rec.SubID, HwidRequest{})
	if err != nil {
		t.Fatalf("missing HWID gate: %v", err)
	}
	if res.Allowed || !res.Active || !res.NotSupported {
		t.Fatalf("missing HWID should be denied as not supported: %+v", res)
	}

	firstRaw := "device-one"
	for _, raw := range []string{firstRaw, "device-two"} {
		res, err = svc.EnforceHwidForSubID(rec.SubID, HwidRequest{
			Hwid:        raw,
			UserAgent:   "Happ/1.0",
			DeviceOS:    "android",
			OsVersion:   "15",
			DeviceModel: raw + "-model",
		})
		if err != nil {
			t.Fatalf("register %s: %v", raw, err)
		}
		if !res.Allowed {
			t.Fatalf("register %s denied: %+v", raw, res)
		}
	}

	res, err = svc.EnforceHwidForSubID(rec.SubID, HwidRequest{Hwid: "device-three"})
	if err != nil {
		t.Fatalf("third HWID gate: %v", err)
	}
	if res.Allowed || !res.MaxDevicesReached || !res.LimitReached {
		t.Fatalf("third unique HWID should be denied after limit: %+v", res)
	}

	res, err = svc.EnforceHwidForSubID(rec.SubID, HwidRequest{
		Hwid:        firstRaw,
		UserAgent:   "Karing/2.0",
		DeviceOS:    "ios",
		OsVersion:   "18",
		DeviceModel: "updated-model",
	})
	if err != nil {
		t.Fatalf("existing HWID after full limit: %v", err)
	}
	if !res.Allowed || !res.LimitReached {
		t.Fatalf("existing registered HWID should pass after limit: %+v", res)
	}

	var hashes []string
	if err := database.GetDB().Model(&model.ClientHwid{}).Pluck("hwid_hash", &hashes).Error; err != nil {
		t.Fatalf("pluck hashes: %v", err)
	}
	if len(hashes) != 2 {
		t.Fatalf("stored HWIDs = %d, want 2", len(hashes))
	}
	for _, h := range hashes {
		if h == firstRaw || h == "device-two" || len(h) != 64 {
			t.Fatalf("raw HWID leaked or invalid hash stored: %q", h)
		}
	}

	list, err := svc.ListClientHwids(rec.Email)
	if err != nil {
		t.Fatalf("list HWIDs: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("list count = %d, want 2", len(list))
	}
	foundUpdated := false
	for _, row := range list {
		if len(row.Fingerprint) != hwidFingerprintLength {
			t.Fatalf("fingerprint length = %d, want %d: %q", len(row.Fingerprint), hwidFingerprintLength, row.Fingerprint)
		}
		if row.DeviceModel == "updated-model" && row.UserAgent == "Karing/2.0" && row.DeviceOS == "ios" && row.OsVersion == "18" {
			foundUpdated = true
			want := hashHwid(firstRaw)[:hwidFingerprintLength]
			if row.Fingerprint != want {
				t.Fatalf("fingerprint = %q, want %q", row.Fingerprint, want)
			}
		}
	}
	if !foundUpdated {
		t.Fatalf("updated HWID metadata missing: %#v", list)
	}

	if err := svc.setClientLimitHwidByEmail(rec.Email, 1); err != nil {
		t.Fatalf("lower limit: %v", err)
	}
	var count int64
	if err := database.GetDB().Model(&model.ClientHwid{}).Where("sub_id = ?", rec.SubID).Count(&count).Error; err != nil {
		t.Fatalf("count after trim: %v", err)
	}
	if count != 1 {
		t.Fatalf("lowered limit should trim stored HWIDs to 1, got %d", count)
	}

	if err := svc.ClearClientHwids(rec.Email); err != nil {
		t.Fatalf("clear HWIDs: %v", err)
	}
	if err := database.GetDB().Model(&model.ClientHwid{}).Where("sub_id = ?", rec.SubID).Count(&count).Error; err != nil {
		t.Fatalf("count after clear: %v", err)
	}
	if count != 0 {
		t.Fatalf("clear should remove all HWIDs, got %d", count)
	}
}

func TestDeleteClientHwid(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}
	db := database.GetDB()

	rec := seedHwidClient(t, 5)
	if _, err := svc.EnforceHwidForSubID(rec.SubID, HwidRequest{Hwid: "device-own"}); err != nil {
		t.Fatalf("register own device: %v", err)
	}
	list, err := svc.ListClientHwids(rec.Email)
	if err != nil || len(list) != 1 {
		t.Fatalf("list own devices: err=%v list=%+v", err, list)
	}
	ownID := list[0].Id

	other := &model.ClientRecord{Email: "other@example.com", SubID: "sub-other", UUID: "33333333-2222-4333-8444-555555555555", Enable: true, LimitHwid: 5}
	if err := db.Create(other).Error; err != nil {
		t.Fatalf("seed other client: %v", err)
	}
	if _, err := svc.EnforceHwidForSubID(other.SubID, HwidRequest{Hwid: "device-foreign"}); err != nil {
		t.Fatalf("register foreign device: %v", err)
	}
	otherList, err := svc.ListClientHwids(other.Email)
	if err != nil || len(otherList) != 1 {
		t.Fatalf("list foreign devices: err=%v list=%+v", err, otherList)
	}
	foreignID := otherList[0].Id

	if err := svc.DeleteClientHwid(rec.Email, foreignID); err == nil {
		t.Fatalf("deleting a foreign sub_id's device id should fail")
	}
	if list, err := svc.ListClientHwids(other.Email); err != nil || len(list) != 1 {
		t.Fatalf("foreign device should survive a cross-sub_id delete attempt: err=%v list=%+v", err, list)
	}

	if err := svc.DeleteClientHwid(rec.Email, 999999); err == nil {
		t.Fatalf("deleting an unknown id should fail")
	}

	if err := svc.DeleteClientHwid(rec.Email, ownID); err != nil {
		t.Fatalf("delete own device: %v", err)
	}
	if list, err := svc.ListClientHwids(rec.Email); err != nil || len(list) != 0 {
		t.Fatalf("own device should be gone: err=%v list=%+v", err, list)
	}
}

func TestClientHwidGateSharedSubIdUsesMaxLimit(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}
	db := database.GetDB()
	subID := "shared-sub"
	if err := db.Create(&model.ClientRecord{Email: "a@ex.com", SubID: subID, UUID: "11111111-2222-4333-8444-555555555555", Enable: true, LimitHwid: 0}).Error; err != nil {
		t.Fatalf("seed anchor: %v", err)
	}
	if err := db.Create(&model.ClientRecord{Email: "b@ex.com", SubID: subID, UUID: "22222222-2222-4333-8444-555555555555", Enable: true, LimitHwid: 2}).Error; err != nil {
		t.Fatalf("seed second: %v", err)
	}
	res, err := svc.EnforceHwidForSubID(subID, HwidRequest{})
	if err != nil || !res.Active || res.Limit != 2 {
		t.Fatalf("expected active gate limit 2 from max row, err=%v res=%+v", err, res)
	}
	if res.Allowed || !res.NotSupported {
		t.Fatalf("missing HWID should be denied: %+v", res)
	}
}

func TestClientHwidSlotStatus(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}
	db := database.GetDB()
	rec := seedHwidClient(t, 1)

	status, found, err := svc.HwidSlotStatusForSubID("no-such-sub")
	if err != nil || found || status != (HwidSlotStatus{}) {
		t.Fatalf("unknown subId = (%+v, %v, %v), want zero status and found=false", status, found, err)
	}

	status, found, err = svc.HwidSlotStatusForSubID(" " + rec.SubID + " ")
	if err != nil || !found {
		t.Fatalf("padded subId = (%+v, %v, %v), want found=true", status, found, err)
	}
	if want := (HwidSlotStatus{Active: true, Limit: 1, Remaining: 1}); status != want {
		t.Fatalf("empty slots = %+v, want %+v", status, want)
	}

	// A shared sub_id takes the highest limit, matching the enforcement gate.
	if err := db.Create(&model.ClientRecord{Email: "second@example.com", SubID: rec.SubID, UUID: "22222222-2222-4333-8444-555555555555", Enable: true, LimitHwid: 3}).Error; err != nil {
		t.Fatalf("seed second client: %v", err)
	}
	for _, hwid := range []string{"device-one", "device-two", "device-three"} {
		if _, err := svc.EnforceHwidForSubID(rec.SubID, HwidRequest{Hwid: hwid}); err != nil {
			t.Fatalf("register %s: %v", hwid, err)
		}
	}
	status, found, err = svc.HwidSlotStatusForSubID(rec.SubID)
	if err != nil || !found {
		t.Fatalf("shared subId = (%+v, %v, %v), want found=true", status, found, err)
	}
	if want := (HwidSlotStatus{Active: true, Limit: 3, Registered: 3, Full: true}); status != want {
		t.Fatalf("full slots = %+v, want %+v", status, want)
	}

	// Deleting the highest-limit client drops the effective limit below the
	// registered count, and remaining must clamp at zero instead of going negative.
	if err := db.Where("email = ?", "second@example.com").Delete(&model.ClientRecord{}).Error; err != nil {
		t.Fatalf("delete second client: %v", err)
	}
	status, _, err = svc.HwidSlotStatusForSubID(rec.SubID)
	if err != nil {
		t.Fatalf("lowered limit: %v", err)
	}
	if want := (HwidSlotStatus{Active: true, Limit: 1, Registered: 3, Remaining: 0, Full: true}); status != want {
		t.Fatalf("over-limit slots = %+v, want %+v", status, want)
	}

	if err := db.Model(&model.ClientRecord{}).Where("sub_id = ?", rec.SubID).UpdateColumn("enable", false).Error; err != nil {
		t.Fatalf("disable clients: %v", err)
	}
	status, found, err = svc.HwidSlotStatusForSubID(rec.SubID)
	if err != nil || found || status != (HwidSlotStatus{}) {
		t.Fatalf("disabled subId = (%+v, %v, %v), want zero status and found=false", status, found, err)
	}
}

func TestEstimatedHwidHash(t *testing.T) {
	a := estimatedHwidHash(HwidRequest{ClientApp: "Happ 2.1.0", DeviceModel: "Pixel 8", DeviceOS: "Android", ClientIP: "1.2.3.4"})
	b := estimatedHwidHash(HwidRequest{ClientApp: "Happ 2.2.0", DeviceModel: "pixel 8", DeviceOS: "android", ClientIP: "9.9.9.9"})
	if a == "" || a[:estimatedHashHalfChars] != b[:estimatedHashHalfChars] {
		t.Fatalf("same app/model/os must share the kind half across versions and IPs: %q vs %q", a, b)
	}
	if a == b {
		t.Fatal("different networks must differ in the network half")
	}
	c := estimatedHwidHash(HwidRequest{ClientApp: "v2rayNG 1.9.0", ClientIP: "1.2.3.4"})
	d := estimatedHwidHash(HwidRequest{ClientApp: "v2rayNG 1.9.1", ClientIP: "1.2.3.99"})
	if c == "" || c != d {
		t.Fatalf("same network and kind must give the same hash: %q vs %q", c, d)
	}
	u1 := estimatedHwidHash(HwidRequest{UserAgent: "MyCustomUA 1.2", ClientIP: "1.2.3.4"})
	u2 := estimatedHwidHash(HwidRequest{UserAgent: "MyCustomUA 3.4", ClientIP: "1.2.3.4"})
	if u1 == "" || u1 != u2 {
		t.Fatalf("a custom user agent must still fingerprint, ignoring its numbers: %q vs %q", u1, u2)
	}
	if estimatedHwidHash(HwidRequest{ClientIP: "1.2.3.4"}) != "" {
		t.Fatal("nothing to fingerprint must give an empty hash")
	}
}

func estimatedRowCount(t *testing.T, subID string) int64 {
	t.Helper()
	var n int64
	if err := database.GetDB().Model(&model.ClientHwid{}).Where("sub_id = ? AND estimated = ?", subID, true).Count(&n).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	return n
}

func TestEstimatedHwidCustomUserAgentIsListed(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}
	rec := seedHwidClient(t, 0)
	if _, err := svc.EnforceHwidForSubID(rec.SubID, HwidRequest{UserAgent: "MyCustomUA", ClientIP: "1.2.3.4"}); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if n := estimatedRowCount(t, rec.SubID); n != 1 {
		t.Fatalf("a custom user agent must still list one device, got %d", n)
	}
}

func TestEstimatedHwidIgnoresUnknownSubscriptions(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}
	seedHwidClient(t, 0)
	if _, err := svc.EnforceHwidForSubID("no-such-sub", HwidRequest{ClientApp: "v2rayNG 1.9.0"}); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if n := estimatedRowCount(t, "no-such-sub"); n != 0 {
		t.Fatalf("scanning a random sub id must not create rows, got %d", n)
	}
}

func TestEstimatedHwidFollowsNetworkChangeButSeparatesConcurrentDevices(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}
	rec := seedHwidClient(t, 0)
	req := HwidRequest{ClientApp: "v2rayNG 1.9.0"}

	req.ClientIP = "1.1.1.1"
	if _, err := svc.EnforceHwidForSubID(rec.SubID, req); err != nil {
		t.Fatalf("gate: %v", err)
	}
	// Right away from another network: a second device active at the same time.
	req.ClientIP = "2.2.2.2"
	if _, err := svc.EnforceHwidForSubID(rec.SubID, req); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if n := estimatedRowCount(t, rec.SubID); n != 2 {
		t.Fatalf("concurrent networks must be two devices, got %d", n)
	}

	// Both go quiet; one phone then reappears on a third network and takes over a quiet row.
	old := nowMillisForTest() - estimatedMigrateAfter - 1000
	if err := database.GetDB().Model(&model.ClientHwid{}).Where("sub_id = ?", rec.SubID).Update("last_seen", old).Error; err != nil {
		t.Fatalf("age rows: %v", err)
	}
	req.ClientIP = "3.3.3.3"
	if _, err := svc.EnforceHwidForSubID(rec.SubID, req); err != nil {
		t.Fatalf("gate: %v", err)
	}
	if n := estimatedRowCount(t, rec.SubID); n != 2 {
		t.Fatalf("a quiet device moving network must not add a row, got %d", n)
	}
}

func TestEstimatedHwidDoesNotCountTowardLimit(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}
	rec := seedHwidClient(t, 1)

	res, err := svc.EnforceHwidForSubID(rec.SubID, HwidRequest{ClientApp: "v2rayNG 1.9.0", DeviceModel: "Pixel 8", DeviceOS: "Android"})
	if err != nil {
		t.Fatalf("gate without HWID: %v", err)
	}
	if res.Allowed || !res.NotSupported {
		t.Fatalf("a request without HWID must stay blocked under a limit: %+v", res)
	}
	list, err := svc.ListClientHwids(rec.Email)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(list) != 1 || !list[0].Estimated || list[0].ClientApp != "v2rayNG 1.9.0" {
		t.Fatalf("expected one estimated device, got %+v", list)
	}

	res, err = svc.EnforceHwidForSubID(rec.SubID, HwidRequest{Hwid: "real-device-1", UserAgent: "x"})
	if err != nil {
		t.Fatalf("gate with HWID: %v", err)
	}
	if !res.Allowed {
		t.Fatalf("the estimated row must not use up the only slot: %+v", res)
	}
}

func TestEstimatedHwidTrackedWithoutLimitAndReplacedByRealHwid(t *testing.T) {
	initClientHwidTestDB(t)
	svc := &ClientService{}
	rec := seedHwidClient(t, 0)
	req := HwidRequest{ClientApp: "v2rayNG 1.9.0", DeviceModel: "Pixel 8", DeviceOS: "Android"}

	for range 2 {
		if _, err := svc.EnforceHwidForSubID(rec.SubID, req); err != nil {
			t.Fatalf("gate: %v", err)
		}
	}
	list, _ := svc.ListClientHwids(rec.Email)
	if len(list) != 1 || !list[0].Estimated {
		t.Fatalf("repeated requests must stay one estimated row, got %+v", list)
	}

	req.Hwid = "real-device-1"
	if _, err := svc.EnforceHwidForSubID(rec.SubID, req); err != nil {
		t.Fatalf("gate with HWID: %v", err)
	}
	list, _ = svc.ListClientHwids(rec.Email)
	if len(list) != 1 || list[0].Estimated {
		t.Fatalf("a real HWID must replace its estimated twin, got %+v", list)
	}
}

func nowMillisForTest() int64 { return time.Now().UnixMilli() }
