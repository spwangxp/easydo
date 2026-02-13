package services

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

type EncryptionService interface {
	Encrypt(plaintext []byte) ([]byte, error)
	Decrypt(ciphertext []byte) ([]byte, error)
	GenerateSSHKey(bits int, comment string) (privateKey, publicKey string, err error)
	ParseSSHPublicKey(publicKey string) (*ssh.PublicKey, error)
	EncryptForTransfer(plaintext []byte) (encrypted, transportKey []byte, err error)
	DecryptFromTransfer(encrypted, transportKey []byte) ([]byte, error)
	GetMasterKey() []byte
}

type AESGCMEncryption struct {
	masterKey []byte
	keyPath   string
}

func NewEncryptionService() (*AESGCMEncryption, error) {
	keyPath := "data/encryption.key"

	var masterKey []byte
	if _, err := os.Stat(keyPath); err == nil {
		data, err := os.ReadFile(keyPath)
		if err != nil {
			return nil, fmt.Errorf("failed to read master key: %w", err)
		}
		masterKey = data
	} else {
		masterKey = make([]byte, 32)
		if _, err := rand.Read(masterKey); err != nil {
			return nil, fmt.Errorf("failed to generate master key: %w", err)
		}

		dir := filepath.Dir(keyPath)
		if err := os.MkdirAll(dir, 0700); err != nil {
			return nil, fmt.Errorf("failed to create key directory: %w", err)
		}

		if err := os.WriteFile(keyPath, masterKey, 0600); err != nil {
			return nil, fmt.Errorf("failed to save master key: %w", err)
		}
	}

	return &AESGCMEncryption{
		masterKey: masterKey,
		keyPath:   keyPath,
	}, nil
}

func (e *AESGCMEncryption) Encrypt(plaintext []byte) ([]byte, error) {
	if len(plaintext) == 0 {
		return nil, errors.New("plaintext cannot be empty")
	}

	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	block, err := aes.NewCipher(e.masterKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	ciphertext := gcm.Seal(iv, iv, plaintext, nil)
	return ciphertext, nil
}

func (e *AESGCMEncryption) Decrypt(ciphertext []byte) ([]byte, error) {
	if len(ciphertext) == 0 {
		return nil, errors.New("ciphertext cannot be empty")
	}

	if len(ciphertext) < 12 {
		return nil, errors.New("invalid ciphertext length")
	}

	iv := ciphertext[:12]
	encryptedData := ciphertext[12:]

	block, err := aes.NewCipher(e.masterKey)
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

func (e *AESGCMEncryption) GenerateSSHKey(bits int, comment string) (privateKey, publicKey string, err error) {
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

func (e *AESGCMEncryption) ParseSSHPublicKey(publicKey string) (*ssh.PublicKey, error) {
	key, _, _, _, err := ssh.ParseAuthorizedKey([]byte(publicKey))
	if err != nil {
		return nil, fmt.Errorf("failed to parse SSH public key: %w", err)
	}
	return &key, nil
}

func (e *AESGCMEncryption) EncryptForTransfer(plaintext []byte) (encrypted, transportKey []byte, err error) {
	if len(plaintext) == 0 {
		return nil, nil, errors.New("plaintext cannot be empty")
	}

	transportKey = make([]byte, 32)
	if _, err := rand.Read(transportKey); err != nil {
		return nil, nil, fmt.Errorf("failed to generate transport key: %w", err)
	}

	block, err := aes.NewCipher(transportKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return nil, nil, fmt.Errorf("failed to generate IV: %w", err)
	}

	encrypted = gcm.Seal(iv, iv, plaintext, nil)

	encryptedTransportKey, err := e.Encrypt(transportKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to encrypt transport key: %w", err)
	}

	return encrypted, encryptedTransportKey, nil
}

func (e *AESGCMEncryption) DecryptFromTransfer(encrypted, encryptedTransportKey []byte) ([]byte, error) {
	if len(encrypted) == 0 {
		return nil, errors.New("encrypted data cannot be empty")
	}

	transportKey, err := e.Decrypt(encryptedTransportKey)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt transport key: %w", err)
	}

	block, err := aes.NewCipher(transportKey)
	if err != nil {
		return nil, fmt.Errorf("failed to create cipher: %w", err)
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("failed to create GCM: %w", err)
	}

	if len(encrypted) < 12 {
		return nil, errors.New("invalid encrypted data length")
	}

	iv := encrypted[:12]
	encryptedData := encrypted[12:]

	plaintext, err := gcm.Open(nil, iv, encryptedData, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to decrypt: %w", err)
	}

	return plaintext, nil
}

func (e *AESGCMEncryption) GetMasterKey() []byte {
	return e.masterKey
}

func Base64Encode(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

func Base64Decode(s string) ([]byte, error) {
	return base64.StdEncoding.DecodeString(s)
}
