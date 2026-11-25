package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
)

const emailTokenBytes = 32

func GenerateEmailToken() (string, error) {
	raw := make([]byte, emailTokenBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate email token: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func HashEmailToken(pepper, purpose, token string) string {
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte("omniflux:email_token:" + purpose + ":"))
	mac.Write([]byte(token))
	return base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
}
