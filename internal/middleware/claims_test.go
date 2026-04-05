package middleware

import (
	"testing"

	"github.com/golang-jwt/jwt/v5"
)

func TestClaimsFromMap(t *testing.T) {
	tests := []struct {
		name       string
		claims     jwt.MapClaims
		wantOK     bool
		wantUserID int64
		wantUser   string
		wantAdmin  bool
	}{
		{
			name:       "valid full claims",
			claims:     jwt.MapClaims{"sub": float64(42), "username": "alice", "is_superadmin": true},
			wantOK:     true,
			wantUserID: 42,
			wantUser:   "alice",
			wantAdmin:  true,
		},
		{
			name:       "valid without superadmin",
			claims:     jwt.MapClaims{"sub": float64(1), "username": "bob"},
			wantOK:     true,
			wantUserID: 1,
			wantUser:   "bob",
			wantAdmin:  false,
		},
		{
			name:   "missing sub",
			claims: jwt.MapClaims{"username": "alice"},
			wantOK: false,
		},
		{
			name:   "missing username",
			claims: jwt.MapClaims{"sub": float64(1)},
			wantOK: false,
		},
		{
			name:   "sub is string not float",
			claims: jwt.MapClaims{"sub": "42", "username": "alice"},
			wantOK: false,
		},
		{
			name:   "username is number not string",
			claims: jwt.MapClaims{"sub": float64(1), "username": float64(123)},
			wantOK: false,
		},
		{
			name:   "empty map",
			claims: jwt.MapClaims{},
			wantOK: false,
		},
		{
			name:       "superadmin is string not bool (ignored gracefully)",
			claims:     jwt.MapClaims{"sub": float64(1), "username": "eve", "is_superadmin": "true"},
			wantOK:     true,
			wantUserID: 1,
			wantUser:   "eve",
			wantAdmin:  false, // string "true" doesn't match bool assertion
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, ok := claimsFromMap(tt.claims)
			if ok != tt.wantOK {
				t.Fatalf("claimsFromMap() ok = %v, want %v", ok, tt.wantOK)
			}
			if !ok {
				return
			}
			if got.UserID != tt.wantUserID {
				t.Errorf("UserID = %d, want %d", got.UserID, tt.wantUserID)
			}
			if got.Username != tt.wantUser {
				t.Errorf("Username = %q, want %q", got.Username, tt.wantUser)
			}
			if got.IsSuperadmin != tt.wantAdmin {
				t.Errorf("IsSuperadmin = %v, want %v", got.IsSuperadmin, tt.wantAdmin)
			}
		})
	}
}
