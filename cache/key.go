package cache

import (
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"strings"
	"unicode"
	"unicode/utf8"
)

// Scope is trusted application metadata. Empty tenant/subject are explicit for
// shared or single-tenant data; never infer them from arbitrary request headers.
type Scope struct {
	App         string
	Environment string
	Namespace   string
	Version     string
	TenantID    string
	SubjectID   string
}

// Key hashes length-framed components so separators cannot cause collisions.
// Version identifies the cached data schema, not a software release version.
func Key(scope Scope, parts ...string) (string, error) {
	if len(parts) == 0 || len(parts) > 16 {
		return "", ErrInvalid
	}

	values := []string{
		scope.App,
		scope.Environment,
		scope.Namespace,
		scope.Version,
		scope.TenantID,
		scope.SubjectID,
	}
	values = append(values, parts...)
	hash := sha256.New()

	for index, value := range values {
		optional := index == 4 || index == 5
		if (!optional && value == "") || len(value) > 1024 ||
			strings.TrimSpace(value) != value ||
			!utf8.ValidString(value) {
			return "", ErrInvalid
		}

		for _, r := range value {
			if unicode.IsControl(r) {
				return "", ErrInvalid
			}
		}

		var length [4]byte
		binary.BigEndian.PutUint32(length[:], uint32(len(value)))

		_, _ = hash.Write(length[:])
		_, _ = hash.Write([]byte(value))
	}

	return "pluto-cache:v1:" + hex.EncodeToString(hash.Sum(nil)), nil
}
