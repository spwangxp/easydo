package models

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"sync"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MasterKeyStatusActive = "active"
)

type MasterKey struct {
	BaseModel
	KeyMaterial string `gorm:"type:text;not null" json:"-"`
	Status      string `gorm:"size:32;not null;default:'active';uniqueIndex" json:"status"`
}

func (MasterKey) TableName() string {
	return "master_keys"
}

var (
	activeMasterKey   []byte
	activeMasterKeyMu sync.RWMutex
)

func GetMasterKey() ([]byte, error) {
	activeMasterKeyMu.RLock()
	defer activeMasterKeyMu.RUnlock()

	if len(activeMasterKey) != 32 {
		return nil, errors.New("master key is not initialized")
	}
	key := make([]byte, len(activeMasterKey))
	copy(key, activeMasterKey)
	return key, nil
}

func setMasterKey(key []byte) error {
	if len(key) != 32 {
		return fmt.Errorf("invalid master key length: %d", len(key))
	}

	copied := make([]byte, len(key))
	copy(copied, key)

	activeMasterKeyMu.Lock()
	activeMasterKey = copied
	activeMasterKeyMu.Unlock()
	return nil
}

func LoadOrCreateMasterKey(db *gorm.DB) ([]byte, error) {
	if db == nil {
		return nil, errors.New("db is nil")
	}

	var keyCopy []byte
	err := db.Transaction(func(tx *gorm.DB) error {
		var record MasterKey
		err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("status = ?", MasterKeyStatusActive).
			First(&record).Error

		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}

		var key []byte
		if errors.Is(err, gorm.ErrRecordNotFound) {
			key = make([]byte, 32)
			if _, readErr := rand.Read(key); readErr != nil {
				return fmt.Errorf("failed to generate master key: %w", readErr)
			}

			record = MasterKey{
				KeyMaterial: base64.StdEncoding.EncodeToString(key),
				Status:      MasterKeyStatusActive,
			}
			if createErr := tx.Create(&record).Error; createErr != nil {
				return createErr
			}
		} else {
			key, err = base64.StdEncoding.DecodeString(record.KeyMaterial)
			if err != nil {
				return fmt.Errorf("failed to decode master key: %w", err)
			}
			if len(key) != 32 {
				return fmt.Errorf("invalid persisted master key length: %d", len(key))
			}
		}

		keyCopy = make([]byte, len(key))
		copy(keyCopy, key)
		return nil
	})
	if err != nil {
		return nil, err
	}

	if err := setMasterKey(keyCopy); err != nil {
		return nil, err
	}

	return keyCopy, nil
}
