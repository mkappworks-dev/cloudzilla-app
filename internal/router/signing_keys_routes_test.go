package router_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/ProtonMail/go-crypto/openpgp"
	"github.com/ProtonMail/go-crypto/openpgp/armor"
	"github.com/ProtonMail/go-crypto/openpgp/packet"
)

func armoredGPGKey(t *testing.T, email string, private bool) string {
	t.Helper()
	cfg := &packet.Config{Algorithm: packet.PubKeyAlgoEdDSA, Time: func() time.Time { return time.Now().Add(-time.Hour) }}
	e, err := openpgp.NewEntity("Rhea", "", email, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := e.AddSigningSubkey(cfg); err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	typ := openpgp.PublicKeyType
	if private {
		typ = openpgp.PrivateKeyType
	}
	w, err := armor.Encode(&buf, typ, nil)
	if err != nil {
		t.Fatal(err)
	}
	if private {
		err = e.SerializePrivate(w, nil)
	} else {
		err = e.Serialize(w)
	}
	if err != nil {
		t.Fatal(err)
	}
	w.Close()
	return buf.String()
}

func postKey(t *testing.T, e transferEnv, path, session string, body map[string]string) (int, []byte) {
	t.Helper()
	raw, _ := json.Marshal(body)
	rr := serve(e.h, importJSONRequest(http.MethodPost, path, session, string(raw)))
	return rr.Code, rr.Body.Bytes()
}

func TestSigningKeys_RequireSignIn(t *testing.T) {
	e := newTransferEnv(t)
	for _, base := range []string{"/api/user/gpg_keys/", "/api/user/ssh_signing_keys/"} {
		for _, tc := range []struct{ method, path string }{
			{http.MethodGet, base}, {http.MethodPost, base}, {http.MethodDelete, base + "1"},
		} {
			if rr := serve(e.h, browserRequest(tc.method, tc.path, "", nil)); rr.Code != http.StatusUnauthorized {
				t.Errorf("%s %s signed out: got %d, want 401", tc.method, tc.path, rr.Code)
			}
		}
	}
}

func TestGPGKeys_Lifecycle(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	armored := armoredGPGKey(t, "rhea@example.com", false)

	if code, _ := postKey(t, e, "/api/user/gpg_keys/", me.session, map[string]string{"title": "x", "public_key": armored}); code != http.StatusForbidden {
		t.Fatalf("add without the password: got %d, want 403", code)
	}
	if n := countRows(t, e.db, `SELECT COUNT(*) FROM gpg_keys WHERE user_id = $1`, me.id); n != 0 {
		t.Fatalf("refused add stored %d keys", n)
	}

	refusals := map[string]struct {
		key  string
		want int
	}{
		"missing key":   {"", http.StatusBadRequest},
		"not a key":     {"hello", http.StatusBadRequest},
		"private block": {armoredGPGKey(t, "x@example.com", true), http.StatusBadRequest},
	}
	for name, tc := range refusals {
		if code, body := postKey(t, e, "/api/user/gpg_keys/", me.session, map[string]string{"public_key": tc.key, "password": "password1"}); code != tc.want {
			t.Errorf("%s: got %d %s, want %d", name, code, body, tc.want)
		}
	}

	code, body := postKey(t, e, "/api/user/gpg_keys/", me.session, map[string]string{"public_key": armored, "password": "password1"})
	if code != http.StatusCreated {
		t.Fatalf("add: got %d %s, want 201", code, body)
	}
	var key struct {
		ID          int64
		KeyID       string `json:"key_id"`
		Fingerprint string
		Emails      []string
		Subkeys     []struct {
			CanSign bool `json:"can_sign"`
		}
	}
	if err := json.Unmarshal(body, &key); err != nil || key.ID == 0 || len(key.Fingerprint) != 40 || len(key.KeyID) != 16 ||
		len(key.Emails) != 1 || len(key.Subkeys) != 2 {
		t.Fatalf("add body = %s (%v), want the stored key with subkeys and emails", body, err)
	}

	if code, _ := postKey(t, e, "/api/user/gpg_keys/", other.session, map[string]string{"public_key": armored, "password": "password1"}); code != http.StatusConflict {
		t.Errorf("same key on another account: got %d, want 409", code)
	}

	list := serve(e.h, importJSONRequest(http.MethodGet, "/api/user/gpg_keys/", me.session, ""))
	if !strings.Contains(list.Body.String(), key.Fingerprint) {
		t.Errorf("list lacks my key: %s", list.Body)
	}
	if theirs := serve(e.h, importJSONRequest(http.MethodGet, "/api/user/gpg_keys/", other.session, "")); strings.TrimSpace(theirs.Body.String()) != "[]" {
		t.Errorf("another user's list = %s, want []", theirs.Body)
	}

	path := "/api/user/gpg_keys/" + strconv.FormatInt(key.ID, 10)
	if rr := serve(e.h, browserRequest(http.MethodDelete, "/api/user/gpg_keys/abc", me.session, nil)); rr.Code != http.StatusBadRequest {
		t.Errorf("non-numeric id: got %d, want 400", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodDelete, path, other.session, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("delete another user's key: got %d, want 404", rr.Code)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM gpg_keys WHERE id = $1`, key.ID) != 1 {
		t.Fatal("another user deleted my key")
	}
	if rr := serve(e.h, browserRequest(http.MethodDelete, path, me.session, nil)); rr.Code != http.StatusNoContent {
		t.Errorf("delete: got %d, want 204", rr.Code)
	}
	for _, table := range []string{"gpg_keys WHERE id", "gpg_subkeys WHERE gpg_key_id", "gpg_key_emails WHERE gpg_key_id"} {
		if n := countRows(t, e.db, `SELECT COUNT(*) FROM `+table+` = $1`, key.ID); n != 0 {
			t.Errorf("delete left %d rows in %s", n, table)
		}
	}
	for _, action := range []string{"user.gpg_key.add", "user.gpg_key.delete"} {
		waitForAudit(t, e.db, me.id, action, 1)
	}
}

func TestSSHSigningKeys_Lifecycle(t *testing.T) {
	e := newTransferEnv(t)
	me, other := e.account(t), e.account(t)
	pub := sshPublicKey(t)
	add := func(session string, extra map[string]string) (int, []byte) {
		body := map[string]string{"title": "signer", "public_key": pub, "password": "password1"}
		for k, v := range extra {
			body[k] = v
		}
		return postKey(t, e, "/api/user/ssh_signing_keys/", session, body)
	}

	if code, _ := add(me.session, map[string]string{"password": ""}); code != http.StatusForbidden {
		t.Fatalf("add without the password: got %d, want 403", code)
	}
	if code, _ := add(me.session, map[string]string{"title": ""}); code != http.StatusBadRequest {
		t.Errorf("add without a title: got %d, want 400", code)
	}
	if code, _ := add(me.session, map[string]string{"public_key": "ssh-ed25519 nope"}); code != http.StatusBadRequest {
		t.Errorf("add garbage: got %d, want 400", code)
	}

	// The same key as an auth key is allowed and leaves ssh_keys alone.
	authBody, _ := json.Marshal(map[string]string{"title": "auth", "public_key": pub, "password": "password1"})
	if rr := serve(e.h, importJSONRequest(http.MethodPost, "/api/user/keys/", me.session, string(authBody))); rr.Code != http.StatusCreated {
		t.Fatalf("add auth key: %d %s", rr.Code, rr.Body)
	}
	code, body := add(me.session, nil)
	if code != http.StatusCreated {
		t.Fatalf("add: got %d %s, want 201", code, body)
	}
	var key struct {
		ID          int64
		Fingerprint string
	}
	if err := json.Unmarshal(body, &key); err != nil || key.ID == 0 || !strings.HasPrefix(key.Fingerprint, "SHA256:") {
		t.Fatalf("add body = %s (%v), want the stored key with a SHA256 fingerprint", body, err)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM ssh_keys WHERE user_id = $1`, me.id) != 1 {
		t.Error("adding a signing key changed ssh_keys")
	}
	if code, _ := add(other.session, nil); code != http.StatusConflict {
		t.Errorf("same key on another account: got %d, want 409", code)
	}

	list := serve(e.h, importJSONRequest(http.MethodGet, "/api/user/ssh_signing_keys/", me.session, ""))
	if !strings.Contains(list.Body.String(), key.Fingerprint) {
		t.Errorf("list lacks my key: %s", list.Body)
	}

	path := "/api/user/ssh_signing_keys/" + strconv.FormatInt(key.ID, 10)
	if rr := serve(e.h, browserRequest(http.MethodDelete, path, other.session, nil)); rr.Code != http.StatusNotFound {
		t.Errorf("delete another user's key: got %d, want 404", rr.Code)
	}
	if rr := serve(e.h, browserRequest(http.MethodDelete, path, me.session, nil)); rr.Code != http.StatusNoContent {
		t.Errorf("delete: got %d, want 204", rr.Code)
	}
	if countRows(t, e.db, `SELECT COUNT(*) FROM ssh_signing_keys WHERE id = $1`, key.ID) != 0 {
		t.Error("delete left the key behind")
	}
	for _, action := range []string{"user.ssh_signing_key.add", "user.ssh_signing_key.delete"} {
		waitForAudit(t, e.db, me.id, action, 1)
	}
}
