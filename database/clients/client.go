package clients

import (
	"encoding/json"
	"fmt"
	logger "github.com/komari-monitor/komari/utils/log"
	"math"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/komari-monitor/komari/database/dbcore"
	"github.com/komari-monitor/komari/database/models"
	"github.com/komari-monitor/komari/database/tasks"
	"github.com/komari-monitor/komari/utils"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

func DeleteClient(clientUuid string) error {
	db := dbcore.GetDBInstance()
	err := db.Delete(&models.Client{}, "uuid = ?", clientUuid).Error
	if err != nil {
		return err
	}
	return nil
}

func SaveClientInfo(update map[string]interface{}) error {
	db := dbcore.GetDBInstance()
	clientUUID, ok := update["uuid"].(string)
	if !ok || clientUUID == "" {
		return fmt.Errorf("invalid client UUID")
	}

	// 确保更新的字段不为空
	if len(update) == 0 {
		return fmt.Errorf("no fields to update")
	}

	update["updated_at"] = time.Now().UTC()
	// Agent input cannot change administrator classification or node creation time.
	for _, key := range []string{"region_manual", "group", "tags", "created_at"} {
		delete(update, key)
	}
	if region, exists := update["region"]; exists {
		// Evaluate the lock in the UPDATE itself to avoid racing an admin edit.
		update["region"] = gorm.Expr("CASE WHEN region_manual = ? THEN region ELSE ? END", true, region)
	}

	toFloat64 := func(value interface{}) (float64, bool) {
		switch typed := value.(type) {
		case float64:
			return typed, true
		case float32:
			return float64(typed), true
		case int:
			return float64(typed), true
		case int8:
			return float64(typed), true
		case int16:
			return float64(typed), true
		case int32:
			return float64(typed), true
		case int64:
			return float64(typed), true
		case uint:
			return float64(typed), true
		case uint8:
			return float64(typed), true
		case uint16:
			return float64(typed), true
		case uint32:
			return float64(typed), true
		case uint64:
			return float64(typed), true
		case json.Number:
			parsed, err := typed.Float64()
			if err != nil {
				return 0, false
			}
			return parsed, true
		default:
			return 0, false
		}
	}

	checkOptionalInt := func(name, key string, maxValue float64) error {
		value, exists := update[key]
		if !exists || value == nil {
			return nil
		}

		numericValue, ok := toFloat64(value)
		if !ok {
			return fmt.Errorf("%s must be a valid number", name)
		}
		if numericValue < 0 || numericValue > maxValue {
			return fmt.Errorf("%s must be a valid non-negative number: %v", name, value)
		}
		return nil
	}

	verify := func(update map[string]interface{}) error {
		if err := checkOptionalInt("Cpu.Cores", "cpu_cores", math.MaxInt-1); err != nil {
			return err
		}
		if err := checkOptionalInt("Cpu.PhysicalCores", "cpu_physical_cores", math.MaxInt-1); err != nil {
			return err
		}
		if err := checkOptionalInt("Ram.Total", "mem_total", math.MaxInt64-1); err != nil {
			return err
		}
		if err := checkOptionalInt("Swap.Total", "swap_total", math.MaxInt64-1); err != nil {
			return err
		}
		if err := checkOptionalInt("Disk.Total", "disk_total", math.MaxInt64-1); err != nil {
			return err
		}
		return nil
	}

	if err := verify(update); err != nil {
		return err
	}

	err := db.Model(&models.Client{}).Where("uuid = ?", clientUUID).Updates(update).Error
	if err != nil {
		return err
	}
	return nil
}

// CreateClient 创建新客户端
func CreateClient() (clientUUID, token string, err error) {
	db := dbcore.GetDBInstance()
	token = utils.GenerateToken()
	clientUUID = uuid.New().String()

	client := models.Client{
		UUID:      clientUUID,
		Token:     token,
		Name:      "client_" + clientUUID[0:8],
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	err = db.Create(&client).Error
	if err != nil {
		return "", "", err
	}
	if err := tasks.AddDefaultOnClientUUID(clientUUID); err != nil {
		logger.ErrorArgs("clients", "Failed to apply default-on ping tasks to new client:", err)
	}
	return clientUUID, token, nil
}

func CreateClientWithName(name string) (clientUUID, token string, err error) {
	if name == "" {
		return CreateClient()
	}
	db := dbcore.GetDBInstance()
	token = utils.GenerateToken()
	clientUUID = uuid.New().String()
	client := models.Client{
		UUID:      clientUUID,
		Token:     token,
		Name:      name,
		CreatedAt: time.Now().UTC(),
		UpdatedAt: time.Now().UTC(),
	}

	err = db.Create(&client).Error
	if err != nil {
		return "", "", err
	}
	if err := tasks.AddDefaultOnClientUUID(clientUUID); err != nil {
		logger.ErrorArgs("clients", "Failed to apply default-on ping tasks to new client:", err)
	}
	return clientUUID, token, nil
}

// CreateClientWithMetadata creates a manually managed client together with its
// initial classification. Keeping this in one insert avoids a briefly
// unclassified node when the admin creates it from the web UI.
func CreateClientWithMetadata(name, group, tags, region string) (clientUUID, token string, err error) {
	classification := map[string]string{"group": group, "tags": tags, "region": region}
	if err := validateClassification(classification); err != nil {
		return "", "", err
	}
	db := dbcore.GetDBInstance()
	token = utils.GenerateToken()
	clientUUID = uuid.New().String()
	if name == "" {
		name = "client_" + clientUUID[0:8]
	}
	client := models.Client{
		UUID:         clientUUID,
		Token:        token,
		Name:         name,
		Group:        classification["group"],
		Tags:         classification["tags"],
		Region:       classification["region"],
		RegionManual: classification["region"] != "",
		CreatedAt:    time.Now().UTC(),
		UpdatedAt:    time.Now().UTC(),
	}
	if err = db.Create(&client).Error; err != nil {
		return "", "", err
	}
	if err := tasks.AddDefaultOnClientUUID(clientUUID); err != nil {
		logger.ErrorArgs("clients", "Failed to apply default-on ping tasks to new client:", err)
	}
	return clientUUID, token, nil
}

// CreateAutoDiscoveryClient 创建一个自动发现客户端，并记录 Agent 上报的主机名，
// 供重装/更新后重新注册时复用身份（见 FindAutoDiscoveryClients）。
func CreateAutoDiscoveryClient(host string) (clientUUID, token string, err error) {
	db := dbcore.GetDBInstance()
	token = utils.GenerateToken()
	clientUUID = uuid.New().String()
	client := models.Client{
		UUID:              clientUUID,
		Token:             token,
		Name:              "Auto-" + host,
		AutoDiscoveryHost: host,
		CreatedAt:         time.Now().UTC(),
		UpdatedAt:         time.Now().UTC(),
	}

	err = db.Create(&client).Error
	if err != nil {
		return "", "", err
	}
	if err := tasks.AddDefaultOnClientUUID(clientUUID); err != nil {
		logger.ErrorArgs("clients", "Failed to apply default-on ping tasks to new client:", err)
	}
	return clientUUID, token, nil
}

// FindAutoDiscoveryClients 返回可能属于同一主机的自动发现节点：
// 包含记录过该主机名的节点，以及名称仍为 "Auto-<host>" 的旧节点（升级前创建、尚未改名）。
func FindAutoDiscoveryClients(host string) ([]models.Client, error) {
	db := dbcore.GetDBInstance()
	var list []models.Client
	err := db.Where("auto_discovery_host = ? OR name = ?", host, "Auto-"+host).Find(&list).Error
	if err != nil {
		return nil, err
	}
	return list, nil
}

/*
// GetAllClients 获取所有客户端配置

	func getAllClients() (clients []models.Client, err error) {
		db := dbcore.GetDBInstance()
		err = db.Find(&clients).Error
		if err != nil {
			return nil, err
		}
		return clients, nil
	}
*/
func GetClientByUUID(uuid string) (client models.Client, err error) {
	db := dbcore.GetDBInstance()
	err = db.Where("uuid = ?", uuid).First(&client).Error
	if err != nil {
		return models.Client{}, err
	}
	return client, nil
}

func GetClientTokenByUUID(uuid string) (token string, err error) {
	db := dbcore.GetDBInstance()
	var client models.Client
	err = db.Where("uuid = ?", uuid).First(&client).Error
	if err != nil {
		return "", err
	}
	return client.Token, nil
}

func GetAllClientBasicInfo() (clients []models.Client, err error) {
	db := dbcore.GetDBInstance()
	err = db.Find(&clients).Error
	if err != nil {
		return nil, err
	}
	return clients, nil
}

func SaveClient(updates map[string]interface{}) error {
	db := dbcore.GetDBInstance()
	clientUUID, ok := updates["uuid"].(string)
	if !ok || clientUUID == "" {
		return fmt.Errorf("invalid client UUID")
	}

	// 确保更新的字段不为空
	if len(updates) == 0 {
		return fmt.Errorf("no fields to update")
	}
	classification := map[string]string{}
	for _, key := range []string{"group", "tags", "region"} {
		if value, exists := updates[key]; exists {
			text, ok := value.(string)
			if !ok {
				return fmt.Errorf("%s must be a string", key)
			}
			classification[key] = text
		}
	}
	if err := validateClassification(classification); err != nil {
		return err
	}
	for key, value := range classification {
		updates[key] = value
	}
	if _, exists := classification["region"]; exists {
		updates["region_manual"] = true
	}
	if auto, exists := updates["region_auto"]; exists {
		enabled, ok := auto.(bool)
		if !ok {
			return fmt.Errorf("region_auto must be a boolean")
		}
		delete(updates, "region_auto")
		if enabled {
			updates["region_manual"] = false
			updates["region"] = ""
		}
	}

	if v, exists := updates["traffic_limit"]; exists {
		if val, ok := v.(float64); ok {
			if val < 0 || val > math.MaxInt64-1 {
				return fmt.Errorf("traffic_limit must be a valid non-negative int64 value, got %v", val)
			}
		}
	}
	if value, exists := updates["expired_at"]; exists {
		switch typed := value.(type) {
		case nil:
			updates["expired_at"] = nil
		case time.Time:
			updates["expired_at"] = typed.UTC()
		case *time.Time:
			if typed == nil {
				updates["expired_at"] = nil
			} else {
				updates["expired_at"] = typed.UTC()
			}
		case string:
			stamp, err := time.Parse(time.RFC3339Nano, typed)
			if err != nil {
				return fmt.Errorf("expired_at must be an RFC3339 timestamp with a timezone: %w", err)
			}
			updates["expired_at"] = stamp.UTC()
		default:
			return fmt.Errorf("expired_at must be an RFC3339 timestamp with a timezone")
		}
	}

	updates["updated_at"] = time.Now().UTC()

	err := db.Model(&models.Client{}).Where("uuid = ?", clientUUID).Updates(updates).Error
	if err != nil {
		return err
	}
	return nil
}

// SaveClientClassifications updates node classifications atomically. Only the
// classification columns are accepted so this endpoint cannot mutate tokens or
// other sensitive client fields through an arbitrary update map.
func SaveClientClassifications(updates []map[string]string) error {
	return saveClientClassifications(dbcore.GetDBInstance(), updates)
}

func validateClassification(update map[string]string) error {
	for _, key := range []string{"group", "tags", "region"} {
		value, exists := update[key]
		if !exists {
			continue
		}
		value = strings.TrimSpace(value)
		if key == "region" && value != "" {
			letters := []rune(value)
			if len(letters) != 2 {
				return fmt.Errorf("region must be a two-letter code or flag")
			}
			isLetter := func(r rune) bool { return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') }
			isFlag := func(r rune) bool { return r >= 0x1f1e6 && r <= 0x1f1ff }
			if isLetter(letters[0]) && isLetter(letters[1]) {
				value = strings.ToUpper(value)
			} else if !isFlag(letters[0]) || !isFlag(letters[1]) {
				return fmt.Errorf("region must be a two-letter code or flag")
			}
		}
		limit := 100
		if key == "tags" {
			limit = 16000
			seen := map[string]bool{}
			tags := []string{}
			for _, tag := range strings.Split(value, ";") {
				tag = strings.TrimSpace(tag)
				if tag != "" && !seen[tag] {
					seen[tag] = true
					tags = append(tags, tag)
				}
			}
			value = strings.Join(tags, ";")
		}
		if !utf8.ValidString(value) || utf8.RuneCountInString(value) > limit || strings.ContainsAny(value, "\x00\r\n") {
			return fmt.Errorf("invalid or oversized classification field: %s", key)
		}
		update[key] = value
	}
	return nil
}

func saveClientClassifications(db *gorm.DB, updates []map[string]string) error {
	if len(updates) == 0 || len(updates) > 1000 {
		return fmt.Errorf("no classification updates")
	}
	seen := map[string]bool{}
	for _, update := range updates {
		if seen[update["uuid"]] {
			return fmt.Errorf("duplicate client UUID")
		}
		seen[update["uuid"]] = true
		if err := validateClassification(update); err != nil {
			return err
		}
	}
	return db.Transaction(func(tx *gorm.DB) error {
		for _, update := range updates {
			clientUUID := update["uuid"]
			if clientUUID == "" {
				return fmt.Errorf("invalid client UUID")
			}
			values := map[string]interface{}{"updated_at": time.Now().UTC()}
			query := tx.Model(&models.Client{}).Where("uuid = ?", clientUUID)
			if expected, exists := update["expected_region_manual"]; exists {
				query = query.Where("region_manual = ?", expected == "true")
			}
			for _, key := range []string{"group", "tags", "region"} {
				if value, ok := update[key]; ok {
					expected, exists := update["expected_"+key]
					if !exists {
						return fmt.Errorf("missing expected value for %s", key)
					}
					// Quote the column name through GORM (group is a SQL keyword).
					if expected == "" {
						query = query.Where(tx.Where(map[string]interface{}{key: ""}).Or(map[string]interface{}{key: nil}))
					} else {
						query = query.Where(map[string]interface{}{key: expected})
					}
					values[key] = value
					if key == "region" {
						values["region_manual"] = update["region_auto"] != "true"
					}
				}
			}
			if len(values) == 1 {
				return fmt.Errorf("no classification fields for client %s", clientUUID)
			}
			result := query.Updates(values)
			if result.Error != nil {
				return result.Error
			}
			if result.RowsAffected == 0 {
				return fmt.Errorf("client changed or was removed; refresh and retry: %s", clientUUID)
			}
		}
		return nil
	})
}
