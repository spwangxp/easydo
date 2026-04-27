package models

import (
	"encoding/json"
	"errors"
	"strings"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	SystemSettingKeyDockerHubMirrors = "dockerhub_mirrors"
)

type SystemSetting struct {
	BaseModel
	Key   string `gorm:"size:128;uniqueIndex;not null" json:"key"`
	Value string `gorm:"type:longtext" json:"value"`
}

func (SystemSetting) TableName() string {
	return "system_settings"
}

func systemSettingKeyScope(key string) func(*gorm.DB) *gorm.DB {
	return func(tx *gorm.DB) *gorm.DB {
		return tx.Where(clause.Eq{Column: clause.Column{Name: "key"}, Value: key})
	}
}

func LoadOrCreateSystemDockerHubMirrors(db *gorm.DB, bootstrap []string) ([]string, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}

	var mirrors []string
	err := db.Transaction(func(tx *gorm.DB) error {
		var setting SystemSetting
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Scopes(systemSettingKeyScope(SystemSettingKeyDockerHubMirrors)).
			First(&setting).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		if errors.Is(err, gorm.ErrRecordNotFound) {
			mirrors = parseDockerHubMirrorList(strings.Join(bootstrap, ","))
			valueBytes, marshalErr := json.Marshal(mirrors)
			if marshalErr != nil {
				return marshalErr
			}
			setting = SystemSetting{Key: SystemSettingKeyDockerHubMirrors, Value: string(valueBytes)}
			if createErr := tx.Create(&setting).Error; createErr != nil {
				return createErr
			}
			return nil
		}

		if strings.TrimSpace(setting.Value) == "" {
			mirrors = []string{}
			return nil
		}
		if err := json.Unmarshal([]byte(setting.Value), &mirrors); err != nil {
			return err
		}
		if mirrors == nil {
			mirrors = []string{}
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return mirrors, nil
}

func parseDockerHubMirrorList(raw string) []string {
	parts := strings.Split(raw, ",")
	mirrors := make([]string, 0, len(parts))
	for _, part := range parts {
		mirror := strings.TrimSpace(part)
		if mirror == "" {
			continue
		}
		mirrors = append(mirrors, mirror)
	}
	return mirrors
}
