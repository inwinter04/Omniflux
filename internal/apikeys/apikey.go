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

// ValidFormat 校验 of_<prefix12>_<secret31> 结构。
// secret 是 base64url 编码,本身可能包含下划线,因此不能按 "_" 拆分,只能按固定长度切。
func ValidFormat(key string) bool {
	const totalLen = len("of_") + 12 + 1 + 31
	if len(key) != totalLen {
		return false
	}
	if !strings.HasPrefix(key, "of_") || key[15] != '_' {
		return false
	}
	_, err := base64.RawURLEncoding.DecodeString(key[16:])
	return err == nil
}
