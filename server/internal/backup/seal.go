package backup

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"

	"golang.org/x/crypto/argon2"
)

// The archive format, in order:
//
//	magic | version | salt | nonce | ciphertext
//
// Sealed with a key derived from the passphrase rather than with Kubby's own key. An
// export only the instance that wrote it could open would be useless in the one situation
// it exists for: restoring into a fresh installation, which has a different key.
const (
	magic   = "KUBBYBAK"
	format  = byte(1)
	saltLen = 16

	// MinPassphrase is the shortest accepted. The passphrase is the only thing between
	// this file and every cluster credential in it, so a short one is not a choice worth
	// offering.
	MinPassphrase = 12
)

// Argon2id parameters. Deliberately heavier than the login hash: an archive is attacked
// offline, at the attacker's leisure, with no rate limit and no lockout in the way.
const (
	argonTime    = 4
	argonMemory  = 128 * 1024 // KiB
	argonThreads = 4
	argonKeyLen  = 32
)

func seal(plaintext []byte, passphrase string) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := rand.Read(salt); err != nil {
		return nil, fmt.Errorf("generate a salt: %w", err)
	}

	gcm, err := cipherFor(passphrase, salt)
	if err != nil {
		return nil, err
	}

	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return nil, fmt.Errorf("generate a nonce: %w", err)
	}

	header := archiveHeader(salt)
	// The header is authenticated as additional data, so the salt and version cannot be
	// altered without the open failing.
	ciphertext := gcm.Seal(nil, nonce, plaintext, header)

	out := make([]byte, 0, len(header)+len(nonce)+len(ciphertext))
	out = append(out, header...)
	out = append(out, nonce...)
	out = append(out, ciphertext...)
	return out, nil
}

func open(sealed []byte, passphrase string) ([]byte, error) {
	if len(sealed) < len(magic)+1+saltLen {
		return nil, ErrNotAnArchive
	}
	if string(sealed[:len(magic)]) != magic {
		return nil, ErrNotAnArchive
	}
	if sealed[len(magic)] != format {
		return nil, fmt.Errorf("this archive is format %d; this build reads %d",
			sealed[len(magic)], format)
	}

	header := sealed[:len(magic)+1+saltLen]
	salt := sealed[len(magic)+1 : len(magic)+1+saltLen]
	rest := sealed[len(header):]

	gcm, err := cipherFor(passphrase, salt)
	if err != nil {
		return nil, err
	}
	if len(rest) < gcm.NonceSize() {
		return nil, ErrNotAnArchive
	}

	nonce, ciphertext := rest[:gcm.NonceSize()], rest[gcm.NonceSize():]

	plaintext, err := gcm.Open(nil, nonce, ciphertext, header)
	if err != nil {
		// Indistinguishable on purpose: a wrong passphrase and a tampered file are the
		// same answer, because saying which would help someone guessing.
		return nil, ErrCannotOpen
	}
	return plaintext, nil
}

func cipherFor(passphrase string, salt []byte) (cipher.AEAD, error) {
	key := argon2.IDKey([]byte(passphrase), salt, argonTime, argonMemory, argonThreads, argonKeyLen)

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("build the cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("build the cipher: %w", err)
	}
	return gcm, nil
}

func archiveHeader(salt []byte) []byte {
	header := make([]byte, 0, len(magic)+1+len(salt))
	header = append(header, magic...)
	header = append(header, format)
	header = append(header, salt...)
	return header
}

var (
	ErrNotAnArchive = errors.New("this file is not a Kubby archive")
	ErrCannotOpen   = errors.New("the archive could not be opened: wrong passphrase, or the file has been altered")
)
