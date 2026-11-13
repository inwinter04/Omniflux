package auth

import "errors"

var ErrPermissionDenied = errors.New("permission denied")

var roleRank = map[string]int{
	"viewer":        1,
	"developer":     2,
	"project_admin": 3,
	"org_admin":     4,
	"org_owner":     5,
}

func RequireRole(principal Principal, minimum string) error {
	got, ok := roleRank[principal.Role]
	if !ok {
		return ErrPermissionDenied
	}
	want, ok := roleRank[minimum]
	if !ok {
		return errors.New("unknown role")
	}
	if got < want {
		return ErrPermissionDenied
	}
	return nil
}
