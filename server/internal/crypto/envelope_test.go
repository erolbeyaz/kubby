package crypto

import (
	"bytes"
	"crypto/rand"
	"errors"
	"testing"
)

func newTestKeyring(t *testing.T, version int) (*Keyring, []byte) {
	t.Helper()
	key := make([]byte, keyLen)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("generate key: %v", err)
	}
	ring, err := NewKeyring(version, key)
	if err != nil {
		t.Fatalf("NewKeyring: %v", err)
	}
	return ring, key
}

func TestSealOpenRoundTrip(t *testing.T) {
	ring, _ := newTestKeyring(t, 1)
	plaintext := []byte("apiVersion: v1\nkind: Config\ntoken: super-secret")
	aad := []byte("cluster-123")

	blob, err := ring.Seal(plaintext, aad)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Contains(blob, plaintext) {
		t.Fatal("ciphertext contains the plaintext")
	}

	got, err := ring.Open(blob, aad)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	if !bytes.Equal(got, plaintext) {
		t.Errorf("Open = %q, want %q", got, plaintext)
	}
}

// Each secret must get its own data key, otherwise rotation and blast radius arguments
// in ADR-009 do not hold.
func TestSealProducesDistinctCiphertextForSameInput(t *testing.T) {
	ring, _ := newTestKeyring(t, 1)

	first, err := ring.Seal([]byte("same"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	second, err := ring.Seal([]byte("same"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}
	if bytes.Equal(first, second) {
		t.Error("two seals of the same plaintext produced identical blobs")
	}
}

// AAD binds a blob to its row: copying it into another record must fail.
func TestOpenRejectsMismatchedAAD(t *testing.T) {
	ring, _ := newTestKeyring(t, 1)

	blob, err := ring.Seal([]byte("kubeconfig"), []byte("cluster-a"))
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	if _, err := ring.Open(blob, []byte("cluster-b")); !errors.Is(err, ErrCiphertext) {
		t.Fatalf("Open with wrong AAD = %v, want ErrCiphertext", err)
	}
}

func TestOpenRejectsTamperedCiphertext(t *testing.T) {
	ring, _ := newTestKeyring(t, 1)

	blob, err := ring.Seal([]byte("kubeconfig"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	for _, pos := range []int{5, 20, len(blob) - 1} {
		tampered := bytes.Clone(blob)
		tampered[pos] ^= 0xFF

		if _, err := ring.Open(tampered, nil); err == nil {
			t.Errorf("Open succeeded after flipping byte %d; GCM must detect this", pos)
		}
	}
}

func TestOpenRejectsTruncatedBlob(t *testing.T) {
	ring, _ := newTestKeyring(t, 1)

	blob, err := ring.Seal([]byte("kubeconfig"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	for _, size := range []int{0, 3, 10, len(blob) - 1} {
		if _, err := ring.Open(blob[:size], nil); err == nil {
			t.Errorf("Open succeeded on a %d-byte blob", size)
		}
	}
}

// Rotation must be incremental: data sealed with a retired key stays readable while it
// waits to be rewrapped (ADR-009).
func TestRotationKeepsRetiredKeysReadable(t *testing.T) {
	oldRing, oldKey := newTestKeyring(t, 1)

	blob, err := oldRing.Seal([]byte("written before rotation"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	newRing, _ := newTestKeyring(t, 2)
	if err := newRing.AddRetiredKey(1, oldKey); err != nil {
		t.Fatalf("AddRetiredKey: %v", err)
	}

	got, err := newRing.Open(blob, nil)
	if err != nil {
		t.Fatalf("Open with retired key: %v", err)
	}
	if string(got) != "written before rotation" {
		t.Errorf("Open = %q", got)
	}
	if !newRing.NeedsRewrap(blob) {
		t.Error("NeedsRewrap = false for a blob sealed with a retired key")
	}

	rewrapped, err := newRing.Seal(got, nil)
	if err != nil {
		t.Fatalf("reseal: %v", err)
	}
	if newRing.NeedsRewrap(rewrapped) {
		t.Error("NeedsRewrap = true for a freshly sealed blob")
	}
}

func TestOpenFailsWhenKeyVersionIsUnknown(t *testing.T) {
	oldRing, _ := newTestKeyring(t, 1)
	blob, err := oldRing.Seal([]byte("data"), nil)
	if err != nil {
		t.Fatalf("Seal: %v", err)
	}

	otherRing, _ := newTestKeyring(t, 2)
	if _, err := otherRing.Open(blob, nil); !errors.Is(err, ErrUnknownKey) {
		t.Fatalf("Open = %v, want ErrUnknownKey", err)
	}
}

func TestNewKeyringRejectsWrongKeyLength(t *testing.T) {
	for _, size := range []int{0, 16, 31, 33, 64} {
		if _, err := NewKeyring(1, make([]byte, size)); !errors.Is(err, ErrKeyRequired) {
			t.Errorf("NewKeyring with a %d-byte key = %v, want ErrKeyRequired", size, err)
		}
	}
}
