package service

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrInvalidHash         = errors.New("the encoded hash is not in the correct format")
	ErrIncompatibleVersion = errors.New("incompatible version of argon2")
)

type argonConfig struct {
	time    uint32
	memory  uint32
	threads uint8
	keyLen  uint32
	saltLen uint32
}

var currentArgonConfig = &argonConfig{
	time:    1,
	memory:  64 * 1024,
	threads: 4,
	keyLen:  32,
	saltLen: 16,
}

// HashPassword hashes a password using Argon2id.
func HashPassword(password string) (string, error) {
	salt := make([]byte, currentArgonConfig.saltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}

	hash := argon2.IDKey([]byte(password), salt, currentArgonConfig.time, currentArgonConfig.memory, currentArgonConfig.threads, currentArgonConfig.keyLen)

	b64Salt := base64.RawStdEncoding.EncodeToString(salt)
	b64Hash := base64.RawStdEncoding.EncodeToString(hash)

	encodedHash := fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, currentArgonConfig.memory, currentArgonConfig.time, currentArgonConfig.threads, b64Salt, b64Hash)
	return encodedHash, nil
}

// VerifyPassword checks if the provided password matches the encoded hash.
// It supports both Argon2id and bcrypt for backwards compatibility.
// If it returns true and needsRehash is true, the caller should re-hash the password with Argon2id and update the DB.
func VerifyPassword(password, encodedHash string) (match bool, needsRehash bool, err error) {
	if strings.HasPrefix(encodedHash, "$2a$") || strings.HasPrefix(encodedHash, "$2b$") {
		// Legacy bcrypt hash
		err = bcrypt.CompareHashAndPassword([]byte(encodedHash), []byte(password))
		if err != nil {
			if errors.Is(err, bcrypt.ErrMismatchedHashAndPassword) {
				return false, false, nil
			}
			return false, false, err
		}
		// Match found, but it's bcrypt, so we should rehash
		return true, true, nil
	}

	if !strings.HasPrefix(encodedHash, "$argon2id$") {
		return false, false, ErrInvalidHash
	}

	vals := strings.Split(encodedHash, "$")
	if len(vals) != 6 {
		return false, false, ErrInvalidHash
	}

	var version int
	_, err = fmt.Sscanf(vals[2], "v=%d", &version)
	if err != nil {
		return false, false, err
	}
	if version != argon2.Version {
		return false, false, ErrIncompatibleVersion
	}

	c := &argonConfig{}
	_, err = fmt.Sscanf(vals[3], "m=%d,t=%d,p=%d", &c.memory, &c.time, &c.threads)
	if err != nil {
		return false, false, err
	}

	salt, err := base64.RawStdEncoding.Strict().DecodeString(vals[4])
	if err != nil {
		return false, false, err
	}
	c.saltLen = uint32(len(salt))

	hash, err := base64.RawStdEncoding.Strict().DecodeString(vals[5])
	if err != nil {
		return false, false, err
	}
	c.keyLen = uint32(len(hash))

	comparisonHash := argon2.IDKey([]byte(password), salt, c.time, c.memory, c.threads, c.keyLen)

	if subtle.ConstantTimeCompare(hash, comparisonHash) == 1 {
		// Valid password. Check if parameters need upgrading.
		needsRehash = (c.time != currentArgonConfig.time) || (c.memory != currentArgonConfig.memory) || (c.threads != currentArgonConfig.threads)
		return true, needsRehash, nil
	}

	return false, false, nil
}
