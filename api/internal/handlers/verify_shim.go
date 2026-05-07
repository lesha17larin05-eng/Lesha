package handlers

import "github.com/leshalarin/api/internal/prodamus"

func verifySig(secret string, data map[string]any, sig string) bool {
	return prodamus.Verify(secret, data, sig)
}
