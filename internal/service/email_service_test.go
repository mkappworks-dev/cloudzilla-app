package service

import (
	"strings"
	"testing"

	"github.com/mkappworks-dev/cloudzilla-app/internal/config"
)

func TestEmailService_Enabled(t *testing.T) {
	if NewEmailService(config.SMTPConfig{}).Enabled() {
		t.Error("no SMTP host must mean disabled")
	}
	if !NewEmailService(config.SMTPConfig{Host: "smtp.test"}).Enabled() {
		t.Error("an SMTP host must mean enabled")
	}
}

func TestSignupLinkEmail_LinksAndEscapes(t *testing.T) {
	_, body := signupLinkEmail("https://cz.test/register/complete/abc")
	if !strings.Contains(body, `href="https://cz.test/register/complete/abc"`) {
		t.Errorf("body must link to the completion page:\n%s", body)
	}
	_, body = signupLinkEmail(`https://cz.test/"><script>x</script>`)
	if strings.Contains(body, "<script>") {
		t.Errorf("link must be HTML-escaped:\n%s", body)
	}
}

func TestAccountExistsEmail_LinksToSignIn(t *testing.T) {
	_, body := accountExistsEmail("https://cz.test/login")
	if !strings.Contains(body, `href="https://cz.test/login"`) {
		t.Errorf("body must link to sign-in:\n%s", body)
	}
}
