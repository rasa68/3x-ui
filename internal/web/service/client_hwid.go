package service

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"time"

	"github.com/mhsanaei/3x-ui/v3/internal/database"
	"github.com/mhsanaei/3x-ui/v3/internal/database/model"
	"github.com/mhsanaei/3x-ui/v3/internal/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type HwidRequest struct {
	Hwid        string
	UserAgent   string
	DeviceOS    string
	OsVersion   string
	DeviceModel string
	ClientApp   string
	ClientIP    string
}

type HwidGateResult struct {
	Allowed           bool
	Active            bool
	NotSupported      bool
	MaxDevicesReached bool
	LimitReached      bool
	Limit             int
	Registered        int
}

// HwidSlotStatus is the aggregate device-slot view exposed to subscribers:
// counters only, no hwid value or hash, no email, no device metadata.
type HwidSlotStatus struct {
	Active     bool `json:"active" example:"true"`
	Limit      int  `json:"limit" example:"2"`
	Registered int  `json:"registered" example:"1"`
	Remaining  int  `json:"remaining" example:"1"`
	Full       bool `json:"full" example:"false"`
}

const (
	minHwidLength         = 6
	hwidFingerprintLength = 12
)

var errClientHwidWriteNotSerialized = errors.New("client HWID write requires the serialized transaction")

type ClientHwidInfo struct {
	Id          int    `json:"id"`
	FirstSeen   int64  `json:"firstSeen"`
	LastSeen    int64  `json:"lastSeen"`
	UserAgent   string `json:"userAgent"`
	DeviceOS    string `json:"deviceOs"`
	OsVersion   string `json:"osVersion"`
	DeviceModel string `json:"deviceModel"`
	ClientApp   string `json:"clientApp"`
	Estimated   bool   `json:"estimated"`
	Fingerprint string `json:"fingerprint"`
}

func hashHwid(raw string) string {
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func shortHwidFingerprint(hash string) string {
	if len(hash) <= hwidFingerprintLength {
		return hash
	}
	return hash[:hwidFingerprintLength]
}

func trimHwidMeta(s string) string {
	s = strings.TrimSpace(s)
	r := []rune(s)
	if len(r) > 512 {
		return string(r[:512])
	}
	return s
}

func normalizeHwidRequest(req HwidRequest) HwidRequest {
	return HwidRequest{
		Hwid:        strings.TrimSpace(req.Hwid),
		UserAgent:   trimHwidMeta(req.UserAgent),
		DeviceOS:    trimHwidMeta(req.DeviceOS),
		OsVersion:   trimHwidMeta(req.OsVersion),
		DeviceModel: trimHwidMeta(req.DeviceModel),
		ClientApp:   trimHwidMeta(req.ClientApp),
		ClientIP:    strings.TrimSpace(req.ClientIP),
	}
}

func effectiveHwidLimitForSubID(tx *gorm.DB, subID string) (int, error) {
	var limit int
	err := tx.Model(&model.ClientRecord{}).
		Where("sub_id = ? AND enable = ?", subID, true).
		Select("COALESCE(MAX(limit_hwid), 0)").
		Scan(&limit).Error
	return limit, err
}

func (s *ClientService) EnforceHwidForSubID(subID string, req HwidRequest) (HwidGateResult, error) {
	var res HwidGateResult
	subID = strings.TrimSpace(subID)
	if subID == "" {
		res.Allowed = true
		return res, nil
	}

	db := database.GetDB()
	limit, err := effectiveHwidLimitForSubID(db, subID)
	if err != nil {
		return res, err
	}
	req = normalizeHwidRequest(req)
	if limit <= 0 {
		res.Allowed = true
		if len(req.Hwid) >= minHwidLength {
			trackUnlimitedHwid(db, subID, req)
		} else {
			trackEstimatedHwid(db, subID, req)
		}
		return res, nil
	}

	res.Active = true
	res.Limit = limit
	if len(req.Hwid) < minHwidLength {
		res.NotSupported = true
		trackEstimatedHwid(db, subID, req)
		return res, nil
	}
	hwidHash := hashHwid(req.Hwid)

	err = db.Transaction(func(tx *gorm.DB) error {
		limit, err := effectiveHwidLimitForSubID(tx, subID)
		if err != nil {
			return err
		}
		if limit <= 0 {
			res = HwidGateResult{Allowed: true}
			return nil
		}
		res.Active = true
		res.Limit = limit
		now := time.Now().UnixMilli()
		var existing model.ClientHwid
		err = tx.Where("sub_id = ? AND hwid_hash = ?", subID, hwidHash).First(&existing).Error
		if err == nil {
			if err := tx.Model(&model.ClientHwid{}).Where("id = ?", existing.Id).Updates(map[string]any{
				"last_seen": now, "user_agent": req.UserAgent, "device_os": req.DeviceOS, "os_version": req.OsVersion, "device_model": req.DeviceModel, "client_app": req.ClientApp,
			}).Error; err != nil {
				return err
			}
			var count int64
			if err := tx.Model(&model.ClientHwid{}).Where("sub_id = ? AND estimated = ?", subID, false).Count(&count).Error; err != nil {
				return err
			}
			res.Allowed = true
			res.Registered = int(count)
			res.LimitReached = count >= int64(limit)
			return nil
		}
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		var count int64
		if err := tx.Model(&model.ClientHwid{}).Where("sub_id = ? AND estimated = ?", subID, false).Count(&count).Error; err != nil {
			return err
		}
		res.Registered = int(count)
		if count >= int64(limit) {
			res.MaxDevicesReached = true
			res.LimitReached = true
			return nil
		}
		if err := tx.Create(&model.ClientHwid{SubID: subID, HwidHash: hwidHash, FirstSeen: now, LastSeen: now, UserAgent: req.UserAgent, DeviceOS: req.DeviceOS, OsVersion: req.OsVersion, DeviceModel: req.DeviceModel, ClientApp: req.ClientApp}).Error; err != nil {
			return err
		}
		dropEstimatedTwin(tx, subID, req)
		res.Allowed = true
		res.Registered = int(count) + 1
		res.LimitReached = res.Registered >= limit
		return nil
	})
	return res, err
}

// trackUnlimitedHwid lists devices of a sub with no HWID limit in the panel. It is
// best-effort: a failed write must not deny a subscription nothing restricts.
func trackUnlimitedHwid(db *gorm.DB, subID string, req HwidRequest) {
	now := time.Now().UnixMilli()
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "sub_id"}, {Name: "hwid_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_seen", "user_agent", "device_os", "os_version", "device_model", "client_app"}),
	}).Create(&model.ClientHwid{SubID: subID, HwidHash: hashHwid(req.Hwid), FirstSeen: now, LastSeen: now, UserAgent: req.UserAgent, DeviceOS: req.DeviceOS, OsVersion: req.OsVersion, DeviceModel: req.DeviceModel, ClientApp: req.ClientApp}).Error
	if err != nil {
		logger.Warning("track HWID for unlimited subscription failed:", err)
		return
	}
	dropEstimatedTwin(db, subID, req)
}

const (
	estimatedHwidPrefix   = "est:"
	estimatedHwidTTLMs    = 30 * 24 * 60 * 60 * 1000
	maxEstimatedPerSubID  = 20
	estimatedPruneScanMax = 1000
)

// estimatedAppName drops the trailing version token: "Happ 2.1.0" -> "Happ".
func estimatedAppName(app string) string {
	f := strings.Fields(app)
	if len(f) > 1 && len(f[len(f)-1]) > 0 && f[len(f)-1][0] >= '0' && f[len(f)-1][0] <= '9' {
		f = f[:len(f)-1]
	}
	return strings.Join(f, " ")
}

func ipPrefixForFingerprint(raw string) string {
	ip := net.ParseIP(raw)
	if ip == nil {
		return ""
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.Mask(net.CIDRMask(24, 32)).String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

// estimatedHwidHash fingerprints a request that sent no HWID. App, model and OS
// identify the device when a model is known; without a model the IP network is
// added so unrelated phones behind different networks are not merged. Returns
// "" when the request carries nothing to fingerprint.
func estimatedHwidHash(req HwidRequest) string {
	app := strings.ToLower(estimatedAppName(req.ClientApp))
	model := strings.ToLower(strings.TrimSpace(req.DeviceModel))
	osName := strings.ToLower(strings.TrimSpace(req.DeviceOS))
	if app == "" && model == "" && osName == "" {
		return ""
	}
	parts := []string{app, model, osName}
	if model == "" {
		parts = append(parts, ipPrefixForFingerprint(req.ClientIP))
	}
	return hashHwid(estimatedHwidPrefix + strings.Join(parts, "|"))
}

// trackEstimatedHwid lists a device that sent no HWID. Best effort and
// display-only: estimated rows are excluded from every limit count.
func trackEstimatedHwid(db *gorm.DB, subID string, req HwidRequest) {
	hash := estimatedHwidHash(req)
	if hash == "" {
		return
	}
	now := time.Now().UnixMilli()
	err := db.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "sub_id"}, {Name: "hwid_hash"}},
		DoUpdates: clause.AssignmentColumns([]string{"last_seen", "user_agent", "device_os", "os_version", "device_model", "client_app"}),
	}).Create(&model.ClientHwid{SubID: subID, HwidHash: hash, FirstSeen: now, LastSeen: now, UserAgent: req.UserAgent, DeviceOS: req.DeviceOS, OsVersion: req.OsVersion, DeviceModel: req.DeviceModel, ClientApp: req.ClientApp, Estimated: true}).Error
	if err != nil {
		logger.Warning("track estimated device failed:", err)
		return
	}
	pruneEstimatedHwids(db, subID, now)
}

// pruneEstimatedHwids drops estimated rows older than the TTL and keeps only
// the newest maxEstimatedPerSubID, so spoofed User-Agents cannot grow the table.
func pruneEstimatedHwids(db *gorm.DB, subID string, now int64) {
	if err := db.Where("sub_id = ? AND estimated = ? AND last_seen < ?", subID, true, now-estimatedHwidTTLMs).Delete(&model.ClientHwid{}).Error; err != nil {
		logger.Warning("prune estimated devices failed:", err)
		return
	}
	var stale []int
	err := db.Model(&model.ClientHwid{}).
		Where("sub_id = ? AND estimated = ?", subID, true).
		Order("last_seen DESC").
		Order("id DESC").
		Limit(estimatedPruneScanMax).
		Offset(maxEstimatedPerSubID).
		Pluck("id", &stale).Error
	if err == nil && len(stale) > 0 {
		_ = db.Where("id IN ?", stale).Delete(&model.ClientHwid{}).Error
	}
}

// dropEstimatedTwin removes the estimated row of a device once it registers
// with a real HWID, so it is not listed twice.
func dropEstimatedTwin(db *gorm.DB, subID string, req HwidRequest) {
	if hash := estimatedHwidHash(req); hash != "" {
		_ = db.Where("sub_id = ? AND hwid_hash = ? AND estimated = ?", subID, hash, true).Delete(&model.ClientHwid{}).Error
	}
}

// HwidSlotStatusForSubID is SELECT-only: it must never write client_hwids or
// last_seen. Enabled-clients scope mirrors the gate, so limit == limit enforced.
func (s *ClientService) HwidSlotStatusForSubID(subID string) (status HwidSlotStatus, found bool, err error) {
	subID = strings.TrimSpace(subID)
	if subID == "" {
		return status, false, nil
	}

	db := database.GetDB()
	var enabled int64
	if err := db.Model(&model.ClientRecord{}).
		Where("sub_id = ? AND enable = ?", subID, true).
		Count(&enabled).Error; err != nil {
		return status, false, err
	}
	if enabled == 0 {
		return status, false, nil
	}

	limit, err := effectiveHwidLimitForSubID(db, subID)
	if err != nil {
		return status, false, err
	}
	if limit <= 0 {
		return status, true, nil
	}

	var registered int64
	if err := db.Model(&model.ClientHwid{}).Where("sub_id = ? AND estimated = ?", subID, false).Count(&registered).Error; err != nil {
		return status, false, err
	}
	status.Active = true
	status.Limit = limit
	status.Registered = int(registered)
	status.Remaining = max(limit-status.Registered, 0)
	status.Full = status.Registered >= limit
	return status, true, nil
}

func (s *ClientService) ListClientHwids(email string) ([]ClientHwidInfo, error) {
	rec, err := s.GetRecordByEmail(nil, email)
	if err != nil {
		return nil, err
	}
	subID := strings.TrimSpace(rec.SubID)
	if subID == "" {
		return nil, nil
	}
	var rows []model.ClientHwid
	if err := database.GetDB().
		Where("sub_id = ?", subID).
		Order("last_seen DESC").
		Order("id DESC").
		Find(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ClientHwidInfo, 0, len(rows))
	for _, r := range rows {
		out = append(out, ClientHwidInfo{
			Id:          r.Id,
			FirstSeen:   r.FirstSeen,
			LastSeen:    r.LastSeen,
			UserAgent:   r.UserAgent,
			DeviceOS:    r.DeviceOS,
			OsVersion:   r.OsVersion,
			DeviceModel: r.DeviceModel,
			ClientApp:   r.ClientApp,
			Estimated:   r.Estimated,
			Fingerprint: shortHwidFingerprint(r.HwidHash),
		})
	}
	return out, nil
}

func (s *ClientService) ClearClientHwids(email string) error {
	rec, err := s.GetRecordByEmail(nil, email)
	if err != nil {
		return err
	}
	subID := strings.TrimSpace(rec.SubID)
	if subID == "" {
		return nil
	}
	return database.GetDB().Where("sub_id = ?", subID).Delete(&model.ClientHwid{}).Error
}

// DeleteClientHwid removes one device, scoped to the client's sub_id: ids
// are a global auto-increment, so an id outside this subscription won't match.
func (s *ClientService) DeleteClientHwid(email string, id int) error {
	rec, err := s.GetRecordByEmail(nil, email)
	if err != nil {
		return err
	}
	subID := strings.TrimSpace(rec.SubID)
	if subID == "" {
		return errors.New("client has no subscription id")
	}
	res := database.GetDB().Where("sub_id = ? AND id = ?", subID, id).Delete(&model.ClientHwid{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return errors.New("device not found")
	}
	return nil
}

// Serialize the limit write and trim with SyncInbound and client deletion.
func (s *ClientService) setClientLimitHwidByEmail(email string, limit int) error {
	return runSerializedTx(func(tx *gorm.DB) error {
		return s.setClientLimitHwidByEmailTx(tx, email, limit)
	})
}

func (s *ClientService) setClientLimitHwidByEmailTx(tx *gorm.DB, email string, limit int) error {
	if !isSerializedTx(tx) {
		return errClientHwidWriteNotSerialized
	}
	if limit < 0 {
		limit = 0
	}
	var rec model.ClientRecord
	if err := tx.Where("email = ?", email).First(&rec).Error; err != nil {
		return err
	}
	if err := tx.Model(&model.ClientRecord{}).Where("id = ?", rec.Id).UpdateColumn("limit_hwid", limit).Error; err != nil {
		return err
	}
	subID := strings.TrimSpace(rec.SubID)
	if subID == "" {
		return nil
	}
	effective, err := effectiveHwidLimitForSubID(tx, subID)
	if err != nil {
		return err
	}
	return trimClientHwidsForSubID(tx, subID, effective)
}

func trimClientHwidsForSubID(tx *gorm.DB, subID string, limit int) error {
	subID = strings.TrimSpace(subID)
	if subID == "" || limit <= 0 {
		return nil
	}
	var keep []int
	if err := tx.Model(&model.ClientHwid{}).
		Where("sub_id = ? AND estimated = ?", subID, false).
		Order("last_seen DESC").
		Order("id DESC").
		Limit(limit).
		Pluck("id", &keep).Error; err != nil {
		return err
	}
	if len(keep) == 0 {
		return tx.Where("sub_id = ? AND estimated = ?", subID, false).Delete(&model.ClientHwid{}).Error
	}
	return tx.Where("sub_id = ? AND estimated = ? AND id NOT IN ?", subID, false, keep).Delete(&model.ClientHwid{}).Error
}

func clearClientHwidsBySubIDTx(tx *gorm.DB, subIDs ...string) error {
	if !isSerializedTx(tx) {
		return errClientHwidWriteNotSerialized
	}
	clean := make([]string, 0, len(subIDs))
	seen := map[string]struct{}{}
	for _, subID := range subIDs {
		subID = strings.TrimSpace(subID)
		if subID == "" {
			continue
		}
		if _, ok := seen[subID]; ok {
			continue
		}
		seen[subID] = struct{}{}
		clean = append(clean, subID)
	}
	for _, batch := range chunkStrings(clean, sqlInChunk) {
		if err := tx.Where("sub_id IN ?", batch).Delete(&model.ClientHwid{}).Error; err != nil {
			return err
		}
	}
	return nil
}
