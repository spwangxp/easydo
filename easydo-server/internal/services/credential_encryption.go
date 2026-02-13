package services

import (
	"encoding/json"
	"errors"
	"fmt"

	"easydo-server/internal/models"
)

type CredentialEncryptionService struct{}

func NewCredentialEncryptionService() *CredentialEncryptionService {
	return &CredentialEncryptionService{}
}

func (s *CredentialEncryptionService) EncryptCredentialData(data map[string]interface{}) (string, string, error) {
	jsonData, err := json.Marshal(data)
	if err != nil {
		return "", "", fmt.Errorf("failed to marshal credential data: %w", err)
	}

	encrypted, err := models.EncryptSecret(string(jsonData))
	if err != nil {
		return "", "", fmt.Errorf("failed to encrypt credential data: %w", err)
	}

	return encrypted, "", nil
}

func (s *CredentialEncryptionService) DecryptCredentialData(encryptedData, iv string) (map[string]interface{}, error) {
	if encryptedData == "" {
		return nil, errors.New("encrypted data is empty")
	}

	decrypted, err := models.DecryptSecret(encryptedData)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt credential data: %w", err)
	}

	var data map[string]interface{}
	if err := json.Unmarshal(decrypted, &data); err != nil {
		return nil, fmt.Errorf("failed to unmarshal credential data: %w", err)
	}

	return data, nil
}
