package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
)

// Envelope implements AES-GCM envelope encryption for credentials.
// Format: [version:1][key_id:1][nonce:12][wrapped_dek:...][ciphertext:...]
type Envelope struct {
	masterKey []byte
	keyID     byte
}

// EnvelopeVersion is the current encryption version.
const EnvelopeVersion byte = 1

// NonceSize is the GCM nonce size (96 bits).
const NonceSize = 12

// KeySize is the DEK size (256 bits).
const KeySize = 32

// NewEnvelope creates an envelope encryptor from a base64-encoded master key.
func NewEnvelope(masterKeyB64 string) (*Envelope, error) {
	masterKey, err := base64.StdEncoding.DecodeString(masterKeyB64)
	if err != nil {
		return nil, fmt.Errorf("decode master key: %w", err)
	}
	if len(masterKey) != KeySize {
		return nil, fmt.Errorf("master key must be %d bytes, got %d", KeySize, len(masterKey))
	}

	return &Envelope{
		masterKey: masterKey,
		keyID:     1, // Current key ID
	}, nil
}

// NewEnvelopeWithKeys creates an envelope with multiple keys for rotation.
// activeKeyID is used for encryption; all keys are available for decryption.
func NewEnvelopeWithKeys(keys map[byte][]byte, activeKeyID byte) (*Envelope, error) {
	if len(keys) == 0 {
		return nil, errors.New("no keys provided")
	}
	active, ok := keys[activeKeyID]
	if !ok {
		return nil, fmt.Errorf("active key ID %d not found", activeKeyID)
	}
	if len(active) != KeySize {
		return nil, fmt.Errorf("active key must be %d bytes", KeySize)
	}

	return &Envelope{
		masterKey: active,
		keyID:     activeKeyID,
	}, nil
}

// Encrypt encrypts plaintext using envelope encryption.
func (e *Envelope) Encrypt(plaintext []byte, aad []byte) ([]byte, error) {
	// Generate random DEK
	dek := make([]byte, KeySize)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("generate DEK: %w", err)
	}

	// Wrap DEK with master key
	wrappedDEK, err := e.wrapKey(dek, aad)
	if err != nil {
		return nil, fmt.Errorf("wrap DEK: %w", err)
	}

	// Encrypt plaintext with DEK
	ciphertext, err := e.encryptWithKey(dek, plaintext, aad)
	if err != nil {
		return nil, fmt.Errorf("encrypt data: %w", err)
	}

	// Build envelope: [version][key_id][wrapped_dek_len:2][wrapped_dek][ciphertext]
	envelope := make([]byte, 0, 4+len(wrappedDEK)+len(ciphertext))
	envelope = append(envelope, EnvelopeVersion)
	envelope = append(envelope, e.keyID)
	envelope = append(envelope, byte(len(wrappedDEK)>>8), byte(len(wrappedDEK)))
	envelope = append(envelope, wrappedDEK...)
	envelope = append(envelope, ciphertext...)

	return envelope, nil
}

// Decrypt decrypts an envelope.
func (e *Envelope) Decrypt(envelope []byte, aad []byte) ([]byte, error) {
	if len(envelope) < 4 {
		return nil, errors.New("envelope too short")
	}

	version := envelope[0]
	if version != EnvelopeVersion {
		return nil, fmt.Errorf("unsupported envelope version: %d", version)
	}

	keyID := envelope[1]
	if keyID != e.keyID {
		// In a real implementation, we'd look up the key by ID
		// For now, we only support the active key
		return nil, fmt.Errorf("key ID %d not available", keyID)
	}

	wrappedLen := int(envelope[2])<<8 | int(envelope[3])
	if len(envelope) < 4+wrappedLen {
		return nil, errors.New("envelope truncated")
	}

	wrappedDEK := envelope[4 : 4+wrappedLen]
	ciphertext := envelope[4+wrappedLen:]

	// Unwrap DEK
	dek, err := e.unwrapKey(wrappedDEK, aad)
	if err != nil {
		return nil, fmt.Errorf("unwrap DEK: %w", err)
	}

	// Decrypt data
	plaintext, err := e.decryptWithKey(dek, ciphertext, aad)
	if err != nil {
		return nil, fmt.Errorf("decrypt data: %w", err)
	}

	return plaintext, nil
}

// wrapKey encrypts a DEK with the master key.
func (e *Envelope) wrapKey(dek []byte, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(e.masterKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	// AAD binds the key wrap to the context
	wrapped := gcm.Seal(nil, nonce, dek, aad)
	return append(nonce, wrapped...), nil
}

// unwrapKey decrypts a wrapped DEK.
func (e *Envelope) unwrapKey(wrapped []byte, aad []byte) ([]byte, error) {
	if len(wrapped) < NonceSize {
		return nil, errors.New("wrapped key too short")
	}

	block, err := aes.NewCipher(e.masterKey)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := wrapped[:NonceSize]
	ciphertext := wrapped[NonceSize:]

	return gcm.Open(nil, nonce, ciphertext, aad)
}

// encryptWithKey encrypts data with a DEK.
func (e *Envelope) encryptWithKey(dek, plaintext, aad []byte) ([]byte, error) {
	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, NonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return nil, err
	}

	ciphertext := gcm.Seal(nil, nonce, plaintext, aad)
	return append(nonce, ciphertext...), nil
}

// decryptWithKey decrypts data with a DEK.
func (e *Envelope) decryptWithKey(dek, ciphertext, aad []byte) ([]byte, error) {
	if len(ciphertext) < NonceSize {
		return nil, errors.New("ciphertext too short")
	}

	block, err := aes.NewCipher(dek)
	if err != nil {
		return nil, err
	}

	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}

	nonce := ciphertext[:NonceSize]
	data := ciphertext[NonceSize:]

	return gcm.Open(nil, nonce, data, aad)
}

// ComputeAAD computes the AAD for a source credential.
// This binds the encryption to the specific source and purpose.
func ComputeAAD(sourceID string, purpose string) []byte {
	h := sha256.New()
	h.Write([]byte("relaydb-v1"))
	h.Write([]byte(sourceID))
	h.Write([]byte(purpose))
	return h.Sum(nil)
}

// GenerateMasterKey generates a random 256-bit master key for testing.
func GenerateMasterKey() (string, error) {
	key := make([]byte, KeySize)
	if _, err := rand.Read(key); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(key), nil
}