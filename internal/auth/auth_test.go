package auth

import (
	"strings"
	"testing"
)

func TestValidatePassword(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name     string
		password string
		wantErr  bool
	}{
		{name: "valid 8 chars lower and digit", password: "abc12345", wantErr: false},
		{name: "valid 128 chars", password: strings.Repeat("a", 120) + "12345678", wantErr: false},
		{name: "valid unicode with letter and digit", password: "密码abc123", wantErr: false},
		{name: "valid symbol with letter and digit", password: "a!@#$%^1", wantErr: false},
		{name: "invalid empty", password: "", wantErr: true},
		{name: "invalid 7 chars", password: "abc1234", wantErr: true},
		{name: "invalid 129 chars", password: strings.Repeat("a", 121) + "12345678", wantErr: true},
		{name: "invalid digits only", password: "12345678", wantErr: true},
		{name: "invalid letters only", password: "abcdefgh", wantErr: true},
		{name: "invalid leading space", password: " abc12345", wantErr: true},
		{name: "invalid trailing space", password: "abc12345 ", wantErr: true},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := ValidatePassword(tc.password)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ValidatePassword(%q) error = %v, wantErr = %v", tc.password, err, tc.wantErr)
			}
		})
	}
}

func TestGeneratePassword(t *testing.T) {
	t.Parallel()

	ambiguous := "0O1lI"
	for i := 0; i < 20; i++ {
		password, err := GeneratePassword()
		if err != nil {
			t.Fatalf("GeneratePassword() error = %v", err)
		}
		if len(password) != 16 {
			t.Fatalf("GeneratePassword() length = %d, want 16", len(password))
		}
		if err := ValidatePassword(password); err != nil {
			t.Fatalf("generated password %q failed ValidatePassword: %v", password, err)
		}
		if strings.ContainsAny(password, ambiguous) {
			t.Fatalf("generated password %q contains ambiguous character", password)
		}
	}
}

func TestResolveEffectivePermissions(t *testing.T) {
	t.Parallel()

	t.Run("pending without group or overrides is empty", func(t *testing.T) {
		t.Parallel()
		perms := ResolveEffectivePermissions("pending", nil, false, nil, nil)
		if len(perms) != 0 {
			t.Fatalf("pending permissions = %v, want empty", perms)
		}
	})

	t.Run("viewer preset boundary", func(t *testing.T) {
		t.Parallel()
		perms := ResolveEffectivePermissions("viewer", nil, false, nil, nil)
		if !perms[PermDashboardRead] || !perms[PermUsageRead] {
			t.Fatalf("viewer missing expected read permissions: %v", perms)
		}
		if perms[PermAuditRead] || perms[PermUsageExport] || perms[PermUserManage] {
			t.Fatalf("viewer unexpectedly has restricted permissions: %v", perms)
		}
	})

	t.Run("custom group overrides preset and deny beats allow", func(t *testing.T) {
		t.Parallel()
		groupPerms := []string{PermDashboardRead, PermPromptRead}
		allow := []string{PermPromptManage, PermUsageRead}
		deny := []string{PermPromptRead, PermUsageRead}

		perms := ResolveEffectivePermissions("viewer", groupPerms, true, allow, deny)
		if !perms[PermDashboardRead] {
			t.Fatalf("expected %s from group", PermDashboardRead)
		}
		if !perms[PermPromptManage] {
			t.Fatalf("expected %s from allow", PermPromptManage)
		}
		if perms[PermPromptRead] {
			t.Fatalf("expected %s to be denied", PermPromptRead)
		}
		if perms[PermUsageRead] {
			t.Fatalf("expected deny to take precedence over allow for %s", PermUsageRead)
		}
		if perms[PermAPIKeyRead] {
			t.Fatalf("expected custom group to replace viewer preset %s", PermAPIKeyRead)
		}
	})

	t.Run("org_owner and platform_admin always retain all permissions", func(t *testing.T) {
		t.Parallel()
		all := AllPermissionCodes()
		for _, role := range []string{"org_owner", "platform_admin"} {
			perms := ResolveEffectivePermissions(role, []string{PermDashboardRead}, true, nil, all)
			if len(perms) != len(all) {
				t.Fatalf("%s permission count = %d, want %d", role, len(perms), len(all))
			}
			for _, code := range all {
				if !perms[code] {
					t.Fatalf("%s missing permission %s", role, code)
				}
			}
		}
	})
}
