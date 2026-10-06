package service

import (
	"errors"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

// LDAP and SAML sign-ins both end in SSOService.generateJWT.
func TestSSOService_GenerateJWT_RefusesSuspended(t *testing.T) {
	s := &SSOService{cfg: config.AuthConfig{JWTSecret: "test-secret-32bytes-minimum-len!", JWTExpiry: time.Hour}}
	now := time.Now()
	if _, err := s.generateJWT(&model.User{ID: 1, Username: "u", SuspendedAt: &now}); !errors.Is(err, ErrAccountSuspended) {
		t.Errorf("suspended: got %v, want ErrAccountSuspended", err)
	}
	if _, err := s.generateJWT(&model.User{ID: 1, Username: "u"}); err != nil {
		t.Errorf("active: %v", err)
	}
}
