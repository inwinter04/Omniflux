package auth

import (
	"bytes"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	MinPasswordLength = 8
	MaxPasswordLength = 128
)

var ErrPasswordWeak = errors.New("password does not meet policy")

// ValidatePassword 校验密码是否满足 8–128 个字符、无首尾空白且同时包含字母和数字的要求。
func ValidatePassword(password string) error {
	if password == "" || strings.TrimSpace(password) != password {
		return ErrPasswordWeak
	}
	length := utf8.RuneCountInString(password)
	if length < MinPasswordLength || length > MaxPasswordLength {
		return ErrPasswordWeak
	}
	var hasLetter, hasDigit bool
	for _, r := range password {
		if unicode.IsLetter(r) {
			hasLetter = true
		} else if unicode.IsDigit(r) {
			hasDigit = true
		}
		if hasLetter && hasDigit {
			return nil
		}
	}
	return ErrPasswordWeak
}

const argonMemory uint32 = 64 * 1024
const argonTime uint32 = 3
const argonThreads uint8 = 1
const argonKeyLen uint32 = 32
const argonSaltLen uint32 = 16

func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, argonTime, argonMemory, argonThreads, argonKeyLen)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", argonMemory, argonTime, argonThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

func VerifyPassword(encoded, password string) (bool, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return false, errors.New("invalid password hash")
	}
	var memory, time uint32
	var threads uint8
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &memory, &time, &threads); err != nil {
		return false, err
	}
	if time < 1 || threads < 1 {
		return false, errors.New("invalid password hash parameters")
	}
	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil {
		return false, err
	}
	want, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil {
		return false, err
	}
	got := argon2.IDKey([]byte(password), salt, time, memory, threads, uint32(len(want)))
	return subtle.ConstantTimeCompare(got, want) == 1, nil
}

const generatedPasswordLength = 16

var generatedPasswordClasses = [3][]byte{
	[]byte("abcdefghjkmnpqrstuvwxyz"),
	[]byte("ABCDEFGHJKLMNPQRSTUVWXYZ"),
	[]byte("23456789"),
}

// GeneratePassword 生成适合管理员转交用户的一次性初始密码;只使用不易抄错的字符。
func GeneratePassword() (string, error) {
	characters := make([]byte, 0, generatedPasswordLength)
	for _, class := range generatedPasswordClasses {
		index, err := randomIndex(len(class))
		if err != nil {
			return "", err
		}
		characters = append(characters, class[index])
	}

	alphabet := bytes.Join(generatedPasswordClasses[:], nil)
	for len(characters) < generatedPasswordLength {
		index, err := randomIndex(len(alphabet))
		if err != nil {
			return "", err
		}
		characters = append(characters, alphabet[index])
	}

	for index := len(characters) - 1; index > 0; index-- {
		swap, err := randomIndex(index + 1)
		if err != nil {
			return "", err
		}
		characters[index], characters[swap] = characters[swap], characters[index]
	}
	return string(characters), nil
}

func randomIndex(limit int) (int, error) {
	value, err := rand.Int(rand.Reader, big.NewInt(int64(limit)))
	if err != nil {
		return 0, err
	}
	return int(value.Int64()), nil
}
