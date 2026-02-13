package models

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/crypto/ssh"
)

var masterKey []byte
var keyPath = "data/encryption.key"

func init() {
	if _, err := os.Stat(keyPath); err == nil {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			panic(fmt.Sprintf("Failed to read master key: %v", err))
		}
		masterKey = data
	} else {
		masterKey = make([]byte, 32)
		if _, err := rand.Read(masterKey); err != nil {
			panic(fmt.Sprintf("Failed to generate master key: %v", err))
		}
		dir := filepath.Dir(keyPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			panic(fmt.Sprintf("Failed to create key directory: %v", err))
		}
		if err := os.WriteFile(keyPath, masterKey, 0600); err != nil {
			panic(fmt.Sprintf("Failed to save master key: %v", err))
		}
	}
}

func EncryptSecret(plaintext string) (string, error) {
	if len(plaintext) == 0 {
		return "", errors.New("plaintext cannot be empty")
	}

	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("failed to generate IV: %w", err)
	}

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return "", fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", fmt.Errorf("failed to create GCM: %w", err)
	}

	ciphertext := gcm.Seal(iv, iv, []byte(plaintext), nil)
	return base64.StdEncoding.EncodeToString(ciphertext), nil
}

func DecryptSecret(encryptedValue string) ([]byte, error) {
	if len(encryptedValue) == 0 {
		return nil, errors.New("encrypted value cannot be empty")
	}

	ciphertext, err := base64.StdEncoding.DecodeString(encryptedValue)
	if err != nil {
		return nil, fmt.Errorf("failed to decode base64: %w", err)
	}

	if len(ciphertext) < 12 {
		return nil, errors.New("invalid ciphertext length")
	}

	iv := ciphertext[:12]
	encryptedData := ciphertext[12:]

	block, err := aes.NewCipher(masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	plaintext, err := gcm.Open(nil, iv, encryptedData, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

func GenerateSSHKey(bits int, comment string) (privateKey, publicKey string, err error) {
	if bits != 2048 && bits != 4096 {
		bits = 2048
	}

	privateKeyRSA, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate RSA key: %w", err)
	}

	err = privateKeyRSA.Validate()
	if err != nil {
		return "", "", fmt.Errorf("RSA key validation failed: %w", err)
	}

	privateKeyPEM := &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKeyRSA),
	}

	privateKey = string(pem.EncodeToMemory(privateKeyPEM))

	publicKeySSH, err := ssh.NewPublicKey(&privateKeyRSA.PublicKey)
	if err != nil {
		return "", "", fmt.Errorf("failed to generate SSH public key: %w", err)
	}

	publicKey = string(ssh.MarshalAuthorizedKey(publicKeySSH))

	if comment != "" {
		publicKey = fmt.Sprintf("%s %s", publicKey, comment)
	}

	return privateKey, publicKey, nil
}
