package hash

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"

	"golang.org/x/crypto/argon2"
)

type Argon2Hasher struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLen     uint32
	KeyLen      uint32
}

func NewArgon2Hasher() *Argon2Hasher {
	return &Argon2Hasher{Memory: 64 * 1024, Iterations: 3, Parallelism: 2, SaltLen: 16, KeyLen: 32}
}

func (h *Argon2Hasher) Hash(plain string) (string, error) {
	if err := h.validate(); err != nil {
		return "", err
	}
	if h.SaltLen == 0 || h.KeyLen == 0 {
		return "", errors.New("argon2 salt and key lengths must be positive")
	}
	salt := make([]byte, h.SaltLen)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(plain), salt, h.Iterations, h.Memory, h.Parallelism, h.KeyLen)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, h.Memory,
		h.Iterations, h.Parallelism, base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key)), nil
}

func (h *Argon2Hasher) Verify(plain, encoded string) (bool, error) {
	parameters, salt, expected, err := parse(encoded)
	if err != nil {
		return false, err
	}
	actual := argon2.IDKey([]byte(plain), salt, parameters.Iterations, parameters.Memory,
		parameters.Parallelism, uint32(len(expected)))
	return subtle.ConstantTimeCompare(actual, expected) == 1, nil
}

func (h *Argon2Hasher) validate() error {
	if h.Memory == 0 || h.Iterations == 0 || h.Parallelism == 0 {
		return errors.New("argon2 parameters must be positive")
	}
	return nil
}

func parse(encoded string) (*Argon2Hasher, []byte, []byte, error) {
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return nil, nil, nil, errors.New("invalid argon2id hash")
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return nil, nil, nil, errors.New("unsupported argon2id version")
	}
	parameters := new(Argon2Hasher)
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &parameters.Memory,
		&parameters.Iterations, &parameters.Parallelism); err != nil {
		return nil, nil, nil, errors.New("invalid argon2id parameters")
	}
	if err := parameters.validate(); err != nil {
		return nil, nil, nil, err
	}
	salt, err := base64.RawStdEncoding.Strict().DecodeString(parts[4])
	if err != nil || len(salt) == 0 {
		return nil, nil, nil, errors.New("invalid argon2id salt")
	}
	expected, err := base64.RawStdEncoding.Strict().DecodeString(parts[5])
	if err != nil || len(expected) == 0 {
		return nil, nil, nil, errors.New("invalid argon2id key")
	}
	parameters.SaltLen = uint32(len(salt))
	parameters.KeyLen = uint32(len(expected))
	return parameters, salt, expected, nil
}
