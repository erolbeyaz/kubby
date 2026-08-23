package crypto_test

import (
	"bytes"
	"sync"
	"testing"

	"github.com/erolbeyaz/kubby/internal/crypto"
)

// The server opens the same sealed credential from many goroutines at once — a health
// sweep alone fans out six detectors, each building its own client. If anything in the
// keyring were shared and mutable, that is where it would show, and the symptom would be
// exactly what was reported: a decrypt that fails sometimes and succeeds on the retry.
func TestKeyringOpensConcurrently(t *testing.T) {
	key := bytes.Repeat([]byte{0x2b}, 32)
	keyring, err := crypto.NewKeyring(1, key)
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	const aad = "9613d777-a525-466e-87ee-66d2ac1c4fd4"
	plaintext := bytes.Repeat([]byte("kubeconfig payload "), 100)

	sealed, err := keyring.Seal(plaintext, []byte(aad))
	if err != nil {
		t.Fatalf("seal: %v", err)
	}

	var wg sync.WaitGroup
	failures := make(chan error, 64)

	for range 64 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			for range 20 {
				opened, err := keyring.Open(sealed, []byte(aad))
				if err != nil {
					failures <- err
					return
				}
				if !bytes.Equal(opened, plaintext) {
					failures <- errMismatch
					return
				}
			}
		}()
	}
	wg.Wait()
	close(failures)

	for err := range failures {
		t.Fatalf("concurrent open failed: %v", err)
	}
}

// Sealing concurrently has to be safe too: several settings can be saved at once.
func TestKeyringSealsConcurrently(t *testing.T) {
	keyring, err := crypto.NewKeyring(1, bytes.Repeat([]byte{0x5c}, 32))
	if err != nil {
		t.Fatalf("keyring: %v", err)
	}

	var wg sync.WaitGroup
	for i := range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()

			payload := []byte{byte(i)}
			sealed, err := keyring.Seal(payload, []byte("aad"))
			if err != nil {
				t.Errorf("seal: %v", err)
				return
			}
			opened, err := keyring.Open(sealed, []byte("aad"))
			if err != nil {
				t.Errorf("open: %v", err)
				return
			}
			if !bytes.Equal(opened, payload) {
				t.Error("round trip returned different bytes")
			}
		}()
	}
	wg.Wait()
}

var errMismatch = errPlain("open returned different bytes")

type errPlain string

func (e errPlain) Error() string { return string(e) }
