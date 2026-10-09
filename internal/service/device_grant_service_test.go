package service_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/mkappworks-dev/cloudzilla-app/internal/service"
	"github.com/mkappworks-dev/cloudzilla-app/internal/store"
	"github.com/mkappworks-dev/cloudzilla-app/internal/testutil"
)

func newDeviceSvc(t *testing.T) (*service.DeviceGrantService, *service.AccessTokenService, int64, string) {
	t.Helper()
	db := testutil.OpenTestDB(t)
	stores := store.New(db)
	suffix := testutil.UniqueSuffix(t)
	uid := testutil.SeedUser(t, db, suffix)
	ip := "ip_" + suffix
	t.Cleanup(func() { testutil.Exec(t, db, `DELETE FROM device_grants WHERE requester_ip = $1`, ip) })
	return service.NewDeviceGrantService(stores.DeviceGrant, stores.AccessToken),
		service.NewAccessTokenService(stores.AccessToken, stores.User), uid, ip
}

func TestDeviceGrantService_Create(t *testing.T) {
	svc, _, _, ip := newDeviceSvc(t)
	ctx := context.Background()

	dc, err := svc.Create(ctx, ip, nil, "laptop")
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if dc.ExpiresIn != 900 || dc.Interval != 5 || len(dc.DeviceCode) != 64 {
		t.Errorf("DeviceCode = %+v; want expires 900, interval 5, 64-char hex", dc)
	}
	if len(dc.UserCode) != 9 || dc.UserCode[4] != '-' || strings.Trim(dc.UserCode, "BCDFGHJKLMNPQRSTVWXZ-") != "" {
		t.Errorf("UserCode = %q; want XXXX-XXXX from the consonant alphabet", dc.UserCode)
	}

	for _, bad := range [][]string{{"repo:admin"}, {"nonsense"}, {"repo:read", "repo:admin"}} {
		if _, err := svc.Create(ctx, ip, bad, ""); !errors.Is(err, service.ErrInvalidScope) {
			t.Errorf("Create(%v) = %v; want ErrInvalidScope", bad, err)
		}
	}
}

func TestDeviceGrantService_CreateCapsLiveGrants(t *testing.T) {
	svc, _, _, ip := newDeviceSvc(t)
	ctx := context.Background()
	for i := 0; i < 5; i++ {
		if _, err := svc.Create(ctx, ip, nil, ""); err != nil {
			t.Fatalf("Create %d: %v", i, err)
		}
	}
	if _, err := svc.Create(ctx, ip, nil, ""); !errors.Is(err, service.ErrTooManyDeviceGrants) {
		t.Errorf("sixth Create = %v; want ErrTooManyDeviceGrants", err)
	}
}

func TestDeviceGrantService_PollLifecycle(t *testing.T) {
	svc, tokens, uid, ip := newDeviceSvc(t)
	ctx := context.Background()
	dc, _ := svc.Create(ctx, ip, []string{"repo:write", "repo:read"}, "mk-laptop\x1b[31m")

	if _, err := svc.Poll(ctx, "nope"); !errors.Is(err, service.ErrInvalidDeviceGrant) {
		t.Errorf("unknown device code = %v; want ErrInvalidDeviceGrant", err)
	}
	if _, err := svc.Poll(ctx, dc.DeviceCode); !errors.Is(err, service.ErrAuthorizationPending) {
		t.Fatalf("first poll = %v; want ErrAuthorizationPending", err)
	}
	if _, err := svc.Poll(ctx, dc.DeviceCode); !errors.Is(err, service.ErrSlowDown) {
		t.Errorf("immediate second poll = %v; want ErrSlowDown", err)
	}

	if err := svc.Approve(ctx, dc.UserCode, uid, []string{"repo:read"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	pollAt := time.Now().Add(time.Minute)
	svc.SetClock(func() time.Time { return pollAt })
	tok, err := svc.Poll(ctx, dc.DeviceCode)
	if err != nil {
		t.Fatalf("poll after approval: %v", err)
	}
	if !strings.HasPrefix(tok.AccessToken, "czp_") || len(tok.Scopes) != 1 || tok.Scopes[0] != "repo:read" {
		t.Errorf("token = %+v; want a czp_ token with only the narrowed scope", tok)
	}
	if _, _, err := tokens.Validate(ctx, tok.AccessToken); err != nil {
		t.Errorf("Validate: %v", err)
	}
	list, _ := tokens.List(ctx, uid)
	wantName := "cz (mk-laptop[31m) · " + pollAt.UTC().Format("2006-01-02")
	if len(list) != 1 || list[0].Name != wantName || list[0].ExpiresAt != nil {
		t.Errorf("listed tokens = %+v; want one named %q (escape byte stripped), no expiry", list, wantName)
	}
	if _, err := svc.Poll(ctx, dc.DeviceCode); !errors.Is(err, service.ErrInvalidDeviceGrant) {
		t.Errorf("poll after redeeming = %v; want ErrInvalidDeviceGrant", err)
	}
}

func TestDeviceGrantService_DeniedAndExpired(t *testing.T) {
	svc, _, uid, ip := newDeviceSvc(t)
	ctx := context.Background()

	denied, _ := svc.Create(ctx, ip, nil, "")
	if err := svc.Deny(ctx, denied.UserCode, uid); err != nil {
		t.Fatal(err)
	}
	if _, err := svc.Poll(ctx, denied.DeviceCode); !errors.Is(err, service.ErrAccessDenied) {
		t.Errorf("denied poll = %v; want ErrAccessDenied", err)
	}
	if err := svc.Approve(ctx, denied.UserCode, uid, nil); !errors.Is(err, service.ErrDeviceGrantNotFound) {
		t.Errorf("Approve of a denied grant = %v; want ErrDeviceGrantNotFound", err)
	}

	expired, _ := svc.Create(ctx, ip, nil, "")
	svc.SetClock(func() time.Time { return time.Now().Add(16 * time.Minute) })
	if _, err := svc.Poll(ctx, expired.DeviceCode); !errors.Is(err, service.ErrExpiredToken) {
		t.Errorf("expired poll = %v; want ErrExpiredToken", err)
	}
}

func TestDeviceGrantService_ApproveCannotAddScopes(t *testing.T) {
	svc, _, uid, ip := newDeviceSvc(t)
	ctx := context.Background()
	dc, _ := svc.Create(ctx, ip, []string{"repo:read"}, "")
	for _, bad := range [][]string{{"repo:write"}, {"repo:admin"}, {}} {
		if err := svc.Approve(ctx, dc.UserCode, uid, bad); !errors.Is(err, service.ErrInvalidScope) {
			t.Errorf("Approve(%v) = %v; want ErrInvalidScope", bad, err)
		}
	}
	if err := svc.Approve(ctx, strings.ToLower(dc.UserCode), uid, []string{"repo:read"}); err != nil {
		t.Errorf("Approve with a lower-case code: %v", err)
	}
}

func TestDeviceGrantService_ApproveDedupesScopes(t *testing.T) {
	svc, _, uid, ip := newDeviceSvc(t)
	ctx := context.Background()
	dc, _ := svc.Create(ctx, ip, []string{"repo:read", "repo:write"}, "")
	if err := svc.Approve(ctx, dc.UserCode, uid, []string{"repo:read", "repo:read"}); err != nil {
		t.Fatalf("Approve: %v", err)
	}
	pollAt := time.Now().Add(time.Minute)
	svc.SetClock(func() time.Time { return pollAt })
	tok, err := svc.Poll(ctx, dc.DeviceCode)
	if err != nil {
		t.Fatalf("Poll: %v", err)
	}
	if len(tok.Scopes) != 1 || tok.Scopes[0] != "repo:read" {
		t.Errorf("Scopes = %v; want exactly [repo:read]", tok.Scopes)
	}
}
