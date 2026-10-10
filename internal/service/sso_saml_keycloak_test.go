package service

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/model"
)

const (
	keycloakDir      = "testdata/saml/keycloak-26.8.0/"
	keycloakEntityID = "https://cz.test.invalid/auth/saml/metadata"
	keycloakACS      = "https://cz.test.invalid/auth/saml/callback"
)

// The instant inside the captured response's 60-second Conditions window; see the fixture README.
var keycloakValidAt = time.Date(2026, 10, 10, 14, 1, 0, 0, time.UTC)

func saveKeycloakSAML(t *testing.T, svc *SSOService) string {
	t.Helper()
	cert, err := os.ReadFile(keycloakDir + "idp-cert.pem")
	if err != nil {
		t.Fatal(err)
	}
	resp, err := os.ReadFile(keycloakDir + "response.xml")
	if err != nil {
		t.Fatal(err)
	}
	err = svc.SetConfig(context.Background(), "saml", map[string]string{
		model.SAMLKeyEntityID: keycloakEntityID, model.SAMLKeySSOURL: "https://idp.test.invalid/sso",
		model.SAMLKeyACSURL: keycloakACS, model.SAMLKeyCert: string(cert),
	}, true)
	if err != nil {
		t.Fatal(err)
	}
	return string(resp)
}

func TestHandleSAMLCallback_AcceptsARealKeycloakResponse(t *testing.T) {
	svc, _ := newSSOTestService(t)
	resp := saveKeycloakSAML(t, svc)
	svc.now = func() time.Time { return keycloakValidAt }

	u, token, err := callback(svc, resp)
	if err != nil {
		t.Fatal(err)
	}
	if u.Username != "ann" || u.Email != "ann@example.test" || token == "" {
		t.Errorf("provisioned %q <%s>, token issued = %v; want ann <ann@example.test>", u.Username, u.Email, token != "")
	}
}

func TestHandleSAMLCallback_KeycloakResponseExpiresAfterItsWindow(t *testing.T) {
	svc, _ := newSSOTestService(t)
	resp := saveKeycloakSAML(t, svc)
	svc.now = func() time.Time { return keycloakValidAt.Add(2 * time.Minute) }

	if _, _, err := callback(svc, resp); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Errorf("got %v, want an expiry refusal", err)
	}
}
