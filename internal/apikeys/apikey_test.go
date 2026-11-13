package apikeys

import "testing"

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

func TestValidFormatRejectsTampering(t *testing.T) {
	got, err := Generate()
	if err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	if ValidFormat(got.Full + "x") {
		t.Fatal("tampered key accepted")
	}
}
