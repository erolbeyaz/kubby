// Package crypto implements envelope encryption for secrets held at rest.
//
// Per ADR-009 each secret gets its own data key (DEK), which is itself encrypted with
// the key-encryption key (KEK) from the environment or a KMS. Key rotation then only
// has to rewrap the DEKs, not re-encrypt every payload, so it can run incrementally
// and without downtime.
package crypto

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
)

const (
	formatVersion = 1
	keyLen        = 32
	nonceLen      = 12
	// A wrapped 32-byte DEK is the key plus the GCM tag.
	wrappedDEKLen = keyLen + 16
)

var (
	ErrKeyRequired    = errors.New("encryption key is required")
	ErrCiphertext     = errors.New("ciphertext is malformed or was tampered with")
	ErrUnknownVersion = errors.New("unknown envelope format version")
	ErrUnknownKey     = errors.New("no key available for this key version")
)

// Keyring holds the active KEK plus any retired KEKs still needed to decrypt records
// that have not been rewrapped yet.
type Keyring struct {
	activeVersion int
	keys          map[int][]byte
}

// NewKeyring builds a keyring with a single active key.
func NewKeyring(version int, key []byte) (*Keyring, error) {
	if len(key) != keyLen {
		return nil, fmt.Errorf("%w: key must be %d bytes, got %d", ErrKeyRequired, keyLen, len(key))
	}
	return &Keyring{
		activeVersion: version,
		keys:          map[int][]byte{version: key},
	}, nil
}

// AddRetiredKey registers an older KEK for decryption only.
func (k *Keyring) AddRetiredKey(version int, key []byte) error {
	if len(key) != keyLen {
		return fmt.Errorf("%w: retired key must be %d bytes, got %d", ErrKeyRequired, keyLen, len(key))
	}
	if version == k.activeVersion {
		return fmt.Errorf("version %d is the active key, not a retired one", version)
	}
	k.keys[version] = key
	return nil
}

// ActiveVersion reports which key new ciphertext is wrapped with.
func (k *Keyring) ActiveVersion() int { return k.activeVersion }

// Seal encrypts plaintext under a fresh DEK and returns a self-describing blob:
//
//	version(2) | keyVersion(2) | dekNonce(12) | wrappedDEK(48) | payloadNonce(12) | ciphertext
//
// aad binds the ciphertext to its owning record (for example the cluster id), so a blob
// cannot be moved to another row and still decrypt.
func (k *Keyring) Seal(plaintext, aad []byte) ([]byte, error) {
	kek, ok := k.keys[k.activeVersion]
	if !ok {
		return nil, ErrUnknownKey
	}

	dek := make([]byte, keyLen)
	if _, err := rand.Read(dek); err != nil {
		return nil, fmt.Errorf("generate data key: %w", err)
	}
	defer zero(dek)

	wrappedDEK, dekNonce, err := encrypt(kek, dek, nil)
	if err != nil {
		return nil, fmt.Errorf("wrap data key: %w", err)
	}

	ciphertext, payloadNonce, err := encrypt(dek, plaintext, aad)
	if err != nil {
		return nil, fmt.Errorf("encrypt payload: %w", err)
	}

	out := make([]byte, 0, 4+nonceLen+len(wrappedDEK)+nonceLen+len(ciphertext))
	out = binary.BigEndian.AppendUint16(out, formatVersion)
	out = binary.BigEndian.AppendUint16(out, uint16(k.activeVersion))
	out = append(out, dekNonce...)
	out = append(out, wrappedDEK...)
	out = append(out, payloadNonce...)
	out = append(out, ciphertext...)
	return out, nil
}

// Open reverses Seal. A mismatched aad, a modified byte or an unknown key version all
// fail rather than returning wrong plaintext.
func (k *Keyring) Open(blob, aad []byte) ([]byte, error) {
	const headerLen = 4 + nonceLen + wrappedDEKLen + nonceLen
	if len(blob) < headerLen {
		return nil, ErrCiphertext
	}

	if version := binary.BigEndian.Uint16(blob[0:2]); version != formatVersion {
		return nil, fmt.Errorf("%w: %d", ErrUnknownVersion, version)
	}
	keyVersion := int(binary.BigEndian.Uint16(blob[2:4]))

	kek, ok := k.keys[keyVersion]
	if !ok {
		return nil, fmt.Errorf("%w: version %d", ErrUnknownKey, keyVersion)
	}

	offset := 4
	dekNonce := blob[offset : offset+nonceLen]
	offset += nonceLen
	wrappedDEK := blob[offset : offset+wrappedDEKLen]
	offset += wrappedDEKLen
	payloadNonce := blob[offset : offset+nonceLen]
	offset += nonceLen
	ciphertext := blob[offset:]

	dek, err := decrypt(kek, dekNonce, wrappedDEK, nil)
	if err != nil {
		return nil, ErrCiphertext
	}
	defer zero(dek)

	plaintext, err := decrypt(dek, payloadNonce, ciphertext, aad)
	if err != nil {
		return nil, ErrCiphertext
	}
	return plaintext, nil
}

// NeedsRewrap reports whether a blob is still wrapped with a retired key.
func (k *Keyring) NeedsRewrap(blob []byte) bool {
	if len(blob) < 4 {
		return false
	}
	return int(binary.BigEndian.Uint16(blob[2:4])) != k.activeVersion
}

func encrypt(key, plaintext, aad []byte) (ciphertext, nonce []byte, err error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, nil, err
	}
	nonce = make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("generate nonce: %w", err)
	}
	return gcm.Seal(nil, nonce, plaintext, aad), nonce, nil
}

func decrypt(key, nonce, ciphertext, aad []byte) ([]byte, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return gcm.Open(nil, nonce, ciphertext, aad)
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create cipher: %w", err)
	}
	return cipher.NewGCM(block)
}

func zero(b []byte) {
	for i := range b {
		b[i] = 0
	}
}
