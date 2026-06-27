package mcphttp

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
)

// ErrEmptyDiscriminator est retournée quand le discriminant brut est vide.
var ErrEmptyDiscriminator = errors.New("empty discriminator")

// hashDiscriminator dérive un nom de répertoire sûr à partir du discriminant
// brut, via HMAC-SHA256 clé par le secret serveur (contrainte C1 du plan).
// La sortie hex (64 caractères [0-9a-f]) ne peut jamais contenir de séquence
// de path-traversal, quel que soit le contenu de raw.
func hashDiscriminator(secret []byte, raw string) (string, error) {
	if raw == "" {
		return "", ErrEmptyDiscriminator
	}
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte(raw))
	return hex.EncodeToString(mac.Sum(nil)), nil
}
