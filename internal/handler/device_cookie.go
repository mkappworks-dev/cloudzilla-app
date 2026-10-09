package handler

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"strconv"
	"strings"
	"time"
)

// Domain separation keeps a device cookie MAC from being valid for anything else keyed by the JWT secret.
const deviceCookieKeyPrefix = "cz-device-code:"

func deviceCookieMAC(secret, payload string) string {
	m := hmac.New(sha256.New, []byte(deviceCookieKeyPrefix+secret))
	m.Write([]byte(payload))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

// signDeviceCode binds userCode to userID until expiry, so a client can't make up a cookie
// or reuse another user's.
func signDeviceCode(secret string, userID int64, userCode string, expiry time.Time) string {
	payload := base64.RawURLEncoding.EncodeToString([]byte(strconv.FormatInt(userID, 10) + "|" + userCode + "|" + strconv.FormatInt(expiry.Unix(), 10)))
	return payload + "." + deviceCookieMAC(secret, payload)
}

// verifyDeviceCode returns the user code in value when it was signed for userID and hasn't expired.
func verifyDeviceCode(secret, value string, userID int64, now time.Time) (string, bool) {
	payload, mac, ok := strings.Cut(value, ".")
	if !ok || !hmac.Equal([]byte(mac), []byte(deviceCookieMAC(secret, payload))) {
		return "", false
	}
	raw, err := base64.RawURLEncoding.DecodeString(payload)
	if err != nil {
		return "", false
	}
	parts := strings.Split(string(raw), "|")
	if len(parts) != 3 || parts[1] == "" {
		return "", false
	}
	uid, err1 := strconv.ParseInt(parts[0], 10, 64)
	exp, err2 := strconv.ParseInt(parts[2], 10, 64)
	if err1 != nil || err2 != nil || uid != userID || !now.Before(time.Unix(exp, 0)) {
		return "", false
	}
	return parts[1], true
}
