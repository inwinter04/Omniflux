package apikeys

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"strings"
)

const secretBytes = 32

type Generated struct {
	Prefix string
	Secret string
	Full   string
}

func Generate() (Generated, error) {
	raw := make([]byte, secretBytes)
	if _, err := rand.Read(raw); err != nil {
		return Generated{}, err
	}
	encoded := base64.RawURLEncoding.EncodeToString(raw)
	if len(encoded) < 16 {
		return Generated{}, errors.New("api key entropy is too short")
	}
	prefix := encoded[:12]
	secret := encoded[12:]
	full := "of_" + prefix + "_" + secret
	return Generated{Prefix: prefix, Secret: secret, Full: full}, nil
}

func Hash(pepper, fullKey string) string {
	mac := hmac.New(sha256.New, []byte(pepper))
	mac.Write([]byte("omniflux:api_key:"))
	mac.Write([]byte(fullKey))
	return base64.RawStdEncoding.EncodeToString(mac.Sum(nil))
}

func Matches(pepper, fullKey, storedHash string) bool {
	want := []byte(storedHash)
	got := []byte(Hash(pepper, fullKey))
	return subtle.ConstantTimeCompare(got, want) == 1
}

func ValidFormat(key string) bool {
	parts := strings.Split(key, "_")
	return len(parts) == 3 && parts[0] == "of" && len(parts[1]) == 12 && len(parts[2]) == 31
}
