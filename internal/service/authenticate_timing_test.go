package service

import (
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// An unknown email is only as slow as a wrong password if the dummy hash costs
// what real hashes cost.
func TestDummyPasswordHash_IsBcryptAtDefaultCost(t *testing.T) {
	cost, err := bcrypt.Cost(dummyPasswordHash)
	if err != nil {
		t.Fatalf("dummy hash is not a bcrypt hash: %v", err)
	}
	if cost != bcrypt.DefaultCost {
		t.Errorf("dummy hash cost = %d, want %d", cost, bcrypt.DefaultCost)
	}
}
