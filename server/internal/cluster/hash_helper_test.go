package cluster_test

import (
	"testing"

	"github.com/erolbeyaz/kubby/internal/auth"
)

// hashForTest produces a cheap password hash; cost is irrelevant to these tests.
func hashForTest(t *testing.T) (string, error) {
	t.Helper()
	return auth.HashPassword("Tr0ubador&Horse!vault", auth.Argon2Params{
		MemoryKiB: 8 * 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32,
	})
}
