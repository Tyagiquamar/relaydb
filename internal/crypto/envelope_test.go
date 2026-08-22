package crypto

import (
	"bytes"
	"testing"
)

func TestEnvelopeEncryptDecrypt(t *testing.T) {
	key, err := GenerateMasterKey()
	if err != nil {
		t.Fatalf("GenerateMasterKey() error = %v", err)
	}

	env, err := NewEnvelope(key)
	if err != nil {
		t.Fatalf("NewEnvelope() error = %v", err)
	}

	plaintext := []byte("postgres://user:password@host:5432/db")
	aad := ComputeAAD("source-123", "credential")

	ciphertext, err := env.Encrypt(plaintext, aad)
	if err != nil {
		t.Fatalf("Encrypt() error = %v", err)
	}

	if bytes.Equal(ciphertext, plaintext) {
		t.Error("ciphertext should not equal plaintext")
	}

	decrypted, err := env.Decrypt(ciphertext, aad)
	if err != nil {
		t.Fatalf("Decrypt() error = %v", err)
	}

	if !bytes.Equal(decrypted, plaintext) {
		t.Errorf("Decrypt() = %q, want %q", decrypted, plaintext)
	}
}

func TestEnvelopeWrongKey(t *testing.T) {
	key1, _ := GenerateMasterKey()
	key2, _ := GenerateMasterKey()

	env1, _ := NewEnvelope(key1)
	env2, _ := NewEnvelope(key2)

	plaintext := []byte("secret")
	aad := ComputeAAD("source-123", "credential")

	ciphertext, _ := env1.Encrypt(plaintext, aad)

	_, err := env2.Decrypt(ciphertext, aad)
	if err == nil {
		t.Fatal("Decrypt() should fail with wrong key")
	}
}

func TestEnvelopeWrongAAD(t *testing.T) {
	key, _ := GenerateMasterKey()
	env, _ := NewEnvelope(key)

	plaintext := []byte("secret")
	aad1 := ComputeAAD("source-1", "credential")
	aad2 := ComputeAAD("source-2", "credential")

	ciphertext, _ := env.Encrypt(plaintext, aad1)

	_, err := env.Decrypt(ciphertext, aad2)
	if err == nil {
		t.Fatal("Decrypt() should fail with wrong AAD")
	}
}

func TestEnvelopeTampered(t *testing.T) {
	key, _ := GenerateMasterKey()
	env, _ := NewEnvelope(key)

	plaintext := []byte("secret")
	aad := ComputeAAD("source-1", "credential")

	ciphertext, _ := env.Encrypt(plaintext, aad)

	// Tamper with ciphertext
	ciphertext[len(ciphertext)-1] ^= 0xFF

	_, err := env.Decrypt(ciphertext, aad)
	if err == nil {
		t.Fatal("Decrypt() should fail with tampered ciphertext")
	}
}

func TestEnvelopeVersion(t *testing.T) {
	key, _ := GenerateMasterKey()
	env, _ := NewEnvelope(key)

	plaintext := []byte("secret")
	aad := ComputeAAD("source-1", "credential")

	ciphertext, _ := env.Encrypt(plaintext, aad)

	// Check version byte
	if ciphertext[0] != EnvelopeVersion {
		t.Errorf("version = %d, want %d", ciphertext[0], EnvelopeVersion)
	}
}

func TestComputeAAD(t *testing.T) {
	aad1 := ComputeAAD("source-1", "credential")
	aad2 := ComputeAAD("source-1", "credential")
	aad3 := ComputeAAD("source-2", "credential")

	if !bytes.Equal(aad1, aad2) {
		t.Error("same inputs should produce same AAD")
	}
	if bytes.Equal(aad1, aad3) {
		t.Error("different inputs should produce different AAD")
	}
}
