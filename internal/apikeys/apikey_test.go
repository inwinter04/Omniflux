package apikeys

import (
	"strings"
	"testing"
)

func TestGenerateHashAndMatches(t *testing.T) {
	got, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if !ValidFormat(got.Full) {
		t.Fatalf("ValidFormat(%q) = false", got.Full)
	}
	hash := Hash("pepper-at-least-32-characters-long", got.Full)
	if Matches("different-pepper-at-least-32-characters", got.Full, hash) {
		t.Fatal("different pepper matched")
	}
	if !Matches("pepper-at-least-32-characters-long", got.Full, hash) {
		t.Fatal("same pepper did not match")
	}
}

func TestValidFormatSurvivesUnderscoreInSecret(t *testing.T) {
	// secret 是 base64url,可能包含下划线;多次生成确保这类 Key 不会被误判为非法格式。
	underscoreFound := false
	for i := 0; i < 64 && !underscoreFound; i++ {
		got, err := Generate()
		if err != nil {
			t.Fatalf("Generate() error = %v", err)
		}
		if strings.Contains(got.Secret, "_") {
			underscoreFound = true
			if !ValidFormat(got.Full) {
				t.Fatalf("ValidFormat rejected generated key with underscore in secret: %q", got.Full)
			}
		}
	}
	if !underscoreFound {
		t.Skip("no underscore in generated secrets this run")
	}
}

func TestValidFormatRejectsTampering(t *testing.T) {
	got, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if ValidFormat(got.Full + "x") {
		t.Fatal("tampered key accepted")
	}
}
