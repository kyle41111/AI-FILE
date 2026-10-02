package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

const (
	profileName          = "openai_file"
	envelopeVersion      = 1
	envelopeAlgorithmCBC = "aes-256-cbc-hmac-sha256+base64url"
	envelopeAlgorithmGCM = "aes-256-gcm+base64url"
	envelopeAlgorithm    = envelopeAlgorithmCBC
)

type fileEnvelope struct {
	Version    int    `json:"v"`
	Profile    string `json:"profile"`
	Channel    string `json:"channel"`
	Direction  string `json:"direction"`
	ID         string `json:"id"`
	Algorithm  string `json:"alg"`
	Nonce      string `json:"nonce"`
	Ciphertext string `json:"ciphertext"`
	CreatedAt  int64  `json:"created_at"`
}

func deriveTransportKey(secret string) ([]byte, error) {
	secret = strings.TrimSpace(secret)
	if secret == "" {
		return nil, fmt.Errorf("empty transport key")
	}
	if strings.HasPrefix(secret, "base64:") {
		decoded, err := base64.StdEncoding.DecodeString(strings.TrimPrefix(secret, "base64:"))
		if err != nil {
			return nil, err
		}
		if len(decoded) != 32 {
			return nil, fmt.Errorf("base64 transport key must decode to 32 bytes, got %d", len(decoded))
		}
		return decoded, nil
	}
	if decoded, err := hex.DecodeString(secret); err == nil && len(decoded) == 32 {
		return decoded, nil
	}
	sum := sha256.Sum256([]byte(secret))
	return sum[:], nil
}

func encryptEnvelope(direction string, channel string, id string, key []byte, plaintext []byte) (*fileEnvelope, error) {
	return encryptEnvelopeWithAlgorithm(direction, channel, id, key, plaintext, envelopeAlgorithm)
}

func encryptEnvelopeWithAlgorithm(direction string, channel string, id string, key []byte, plaintext []byte, algorithm string) (*fileEnvelope, error) {
	switch algorithm {
	case envelopeAlgorithmCBC:
		return encryptEnvelopeCBC(direction, channel, id, key, plaintext)
	case envelopeAlgorithmGCM:
		return encryptEnvelopeGCM(direction, channel, id, key, plaintext)
	default:
		return nil, fmt.Errorf("unsupported envelope algorithm %q", algorithm)
	}
}

func encryptEnvelopeCBC(direction string, channel string, id string, key []byte, plaintext []byte) (*fileEnvelope, error) {
	iv := make([]byte, aes.BlockSize)
	if _, err := rand.Read(iv); err != nil {
		return nil, err
	}
	env := &fileEnvelope{
		Version:   envelopeVersion,
		Profile:   profileName,
		Channel:   channel,
		Direction: direction,
		ID:        id,
		Algorithm: envelopeAlgorithmCBC,
		Nonce:     base64.RawURLEncoding.EncodeToString(iv),
		CreatedAt: time.Now().Unix(),
	}
	encKey, macKey := deriveEnvelopeKeys(key)
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	padded := pkcs7Pad(plaintext, aes.BlockSize)
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)
	tag := envelopeHMAC(macKey, env.aad(), iv, ciphertext)
	env.Ciphertext = base64.RawURLEncoding.EncodeToString(append(ciphertext, tag...))
	return env, nil
}

func encryptEnvelopeGCM(direction string, channel string, id string, key []byte, plaintext []byte) (*fileEnvelope, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}
	env := &fileEnvelope{
		Version:   envelopeVersion,
		Profile:   profileName,
		Channel:   channel,
		Direction: direction,
		ID:        id,
		Algorithm: envelopeAlgorithmGCM,
		Nonce:     base64.RawURLEncoding.EncodeToString(nonce),
		CreatedAt: time.Now().Unix(),
	}
	ciphertext := gcm.Seal(nil, nonce, plaintext, env.aad())
	env.Ciphertext = base64.RawURLEncoding.EncodeToString(ciphertext)
	return env, nil
}

func decryptEnvelope(env fileEnvelope, key []byte) ([]byte, error) {
	if env.Version != envelopeVersion {
		return nil, fmt.Errorf("unsupported envelope version %d", env.Version)
	}
	if env.Profile != profileName {
		return nil, fmt.Errorf("unsupported profile %q", env.Profile)
	}
	switch env.Algorithm {
	case envelopeAlgorithmCBC:
		return decryptEnvelopeCBC(env, key)
	case envelopeAlgorithmGCM:
		return decryptEnvelopeGCM(env, key)
	default:
		return nil, fmt.Errorf("unsupported envelope algorithm %q", env.Algorithm)
	}
}

func decryptEnvelopeCBC(env fileEnvelope, key []byte) ([]byte, error) {
	iv, err := base64.RawURLEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, err
	}
	sealed, err := base64.RawURLEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, err
	}
	if len(iv) != aes.BlockSize {
		return nil, fmt.Errorf("invalid iv size %d", len(iv))
	}
	if len(sealed) < sha256.Size+aes.BlockSize || (len(sealed)-sha256.Size)%aes.BlockSize != 0 {
		return nil, fmt.Errorf("invalid ciphertext size %d", len(sealed))
	}
	ciphertext := sealed[:len(sealed)-sha256.Size]
	gotTag := sealed[len(sealed)-sha256.Size:]
	encKey, macKey := deriveEnvelopeKeys(key)
	expectedTag := envelopeHMAC(macKey, env.aad(), iv, ciphertext)
	if !hmac.Equal(gotTag, expectedTag) {
		return nil, fmt.Errorf("invalid envelope hmac")
	}
	block, err := aes.NewCipher(encKey)
	if err != nil {
		return nil, err
	}
	padded := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(padded, ciphertext)
	return pkcs7Unpad(padded, aes.BlockSize)
}

func decryptEnvelopeGCM(env fileEnvelope, key []byte) ([]byte, error) {
	nonce, err := base64.RawURLEncoding.DecodeString(env.Nonce)
	if err != nil {
		return nil, err
	}
	ciphertext, err := base64.RawURLEncoding.DecodeString(env.Ciphertext)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	if len(nonce) != gcm.NonceSize() {
		return nil, fmt.Errorf("invalid nonce size %d", len(nonce))
	}
	return gcm.Open(nil, nonce, ciphertext, env.aad())
}

func (e fileEnvelope) aad() []byte {
	return []byte(fmt.Sprintf("%d|%s|%s|%s|%s|%s", e.Version, e.Profile, e.Channel, e.Direction, e.ID, e.Algorithm))
}

func envelopeJSONL(env *fileEnvelope) ([]byte, error) {
	data, err := json.Marshal(env)
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}

func deriveEnvelopeKeys(key []byte) ([]byte, []byte) {
	encHash := sha256.Sum256(append([]byte("openai_file:enc:"), key...))
	macHash := sha256.Sum256(append([]byte("openai_file:mac:"), key...))
	return encHash[:], macHash[:]
}

func envelopeHMAC(macKey []byte, aad []byte, iv []byte, ciphertext []byte) []byte {
	mac := hmac.New(sha256.New, macKey)
	mac.Write(aad)
	mac.Write(iv)
	mac.Write(ciphertext)
	return mac.Sum(nil)
}

func pkcs7Pad(data []byte, blockSize int) []byte {
	padding := blockSize - len(data)%blockSize
	out := make([]byte, len(data)+padding)
	copy(out, data)
	for i := len(data); i < len(out); i++ {
		out[i] = byte(padding)
	}
	return out
}

func pkcs7Unpad(data []byte, blockSize int) ([]byte, error) {
	if len(data) == 0 || len(data)%blockSize != 0 {
		return nil, fmt.Errorf("invalid padded size %d", len(data))
	}
	padding := int(data[len(data)-1])
	if padding == 0 || padding > blockSize || padding > len(data) {
		return nil, fmt.Errorf("invalid padding")
	}
	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil, fmt.Errorf("invalid padding")
		}
	}
	return data[:len(data)-padding], nil
}
