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

	"golang.org/x/crypto/ssh"
)

func EncryptCredentialPayload(plaintext string) (string, error) {
	if len(plaintext) == 0 {
		return "", errors.New("plaintext cannot be empty")
	}

	iv := make([]byte, 12)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("failed to generate IV: %w", err)
	}

	masterKey, err := GetMasterKey()
	if err != nil {
		return "", fmt.Errorf("failed to load master key: %w", err)
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

func DecryptCredentialPayload(encryptedValue string) ([]byte, error) {
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

	masterKey, err := GetMasterKey()
	if err != nil {
		return nil, fmt.Errorf("failed to load master key: %w", err)
	}

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
