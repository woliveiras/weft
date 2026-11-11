// Package secrets encrypts credentials that operators enter in the console
// (model provider keys, integration tokens) before they reach storage.
//
// A Box holds one primary key, used for every new seal, and any number of
// previous keys that can still open older ciphertexts. Rotating the
// installation key is therefore: configure the new key as primary, keep the
// old one as previous, let the owners of sealed values re-seal them (see
// NeedsReseal), then drop the old key.
//
// Sealed values are AES-256-GCM with a random nonce, encoded as
//
//	v1.<key id>.<base64url(nonce || ciphertext)>
//
// The key id is a short digest of the key, never the key itself. Every seal
// takes associated data (the record the secret belongs to, e.g.
// "llm/providers/openai/apiKey"), so a ciphertext copied onto another record
// fails to open instead of silently granting that record the credential.
package secrets

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// KeySize is the required key length (AES-256).
const KeySize = 32

const version = "v1"

var (
	// ErrNoKey means no encryption key is configured, so secrets cannot be
	// stored. Callers must refuse the write rather than store plain text.
	ErrNoKey = errors.New("secrets: no encryption key configured")
	// ErrUnknownKey means the value was sealed with a key this installation
	// no longer has (rotated out or a different installation).
	ErrUnknownKey = errors.New("secrets: value was sealed with an unknown key")
	// ErrCorrupt means the value is malformed or failed authentication
	// (tampered, truncated, or moved to another record).
	ErrCorrupt = errors.New("secrets: sealed value is corrupt or does not belong to this record")
)

// Box seals and opens secrets. The zero value and nil have no key: Seal
// returns ErrNoKey.
type Box struct {
	primary string // key id
	aeads   map[string]cipher.AEAD
}

// NewBox builds a box from a primary key and optional previous keys.
func NewBox(primary []byte, previous ...[]byte) (*Box, error) {
	b := &Box{aeads: map[string]cipher.AEAD{}}
	add := func(k []byte) (string, error) {
		if len(k) != KeySize {
			return "", fmt.Errorf("secrets: key must be %d bytes, got %d", KeySize, len(k))
		}
		blk, err := aes.NewCipher(k)
		if err != nil {
			return "", err
		}
		a, err := cipher.NewGCM(blk)
		if err != nil {
			return "", err
		}
		id := KeyID(k)
		b.aeads[id] = a
		return id, nil
	}
	id, err := add(primary)
	if err != nil {
		return nil, err
	}
	b.primary = id
	for _, k := range previous {
		if _, err := add(k); err != nil {
			return nil, fmt.Errorf("previous key: %w", err)
		}
	}
	return b, nil
}

// KeyID is the public identifier of a key: 8 hex characters of its SHA-256.
func KeyID(k []byte) string {
	sum := sha256.Sum256(k)
	return hex.EncodeToString(sum[:4])
}

// Enabled reports whether the box can seal.
func (b *Box) Enabled() bool { return b != nil && b.primary != "" }

// PrimaryKeyID identifies the key new seals use ("" without a key).
func (b *Box) PrimaryKeyID() string {
	if b == nil {
		return ""
	}
	return b.primary
}

// Seal encrypts plaintext bound to aad.
func (b *Box) Seal(plaintext []byte, aad string) (string, error) {
	if !b.Enabled() {
		return "", ErrNoKey
	}
	a := b.aeads[b.primary]
	nonce := make([]byte, a.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return "", fmt.Errorf("secrets: nonce: %w", err)
	}
	ct := a.Seal(nonce, nonce, plaintext, []byte(aad))
	return version + "." + b.primary + "." + base64.RawURLEncoding.EncodeToString(ct), nil
}

// Open decrypts a sealed value that was bound to aad.
func (b *Box) Open(sealed, aad string) ([]byte, error) {
	if b == nil || len(b.aeads) == 0 {
		return nil, ErrNoKey
	}
	ver, rest, ok := strings.Cut(sealed, ".")
	if !ok || ver != version {
		return nil, ErrCorrupt
	}
	kid, data, ok := strings.Cut(rest, ".")
	if !ok {
		return nil, ErrCorrupt
	}
	a, ok := b.aeads[kid]
	if !ok {
		return nil, fmt.Errorf("%w (key id %s)", ErrUnknownKey, kid)
	}
	raw, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil || len(raw) < a.NonceSize()+a.Overhead() {
		return nil, ErrCorrupt
	}
	pt, err := a.Open(nil, raw[:a.NonceSize()], raw[a.NonceSize():], []byte(aad))
	if err != nil {
		return nil, ErrCorrupt
	}
	return pt, nil
}

// NeedsReseal reports whether sealed was produced with a key other than the
// primary one (it opens, but should be re-sealed to finish a rotation).
func (b *Box) NeedsReseal(sealed string) bool {
	if !b.Enabled() {
		return false
	}
	parts := strings.SplitN(sealed, ".", 3)
	return len(parts) == 3 && parts[1] != b.primary
}

// ParseKey decodes a key written as base64 (standard or URL alphabet, with
// or without padding) or as 64 hex characters.
func ParseKey(s string) ([]byte, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil, errors.New("secrets: empty key")
	}
	if len(s) == 2*KeySize {
		if k, err := hex.DecodeString(s); err == nil {
			return k, nil
		}
	}
	for _, enc := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding, base64.URLEncoding, base64.RawURLEncoding} {
		if k, err := enc.DecodeString(s); err == nil {
			if len(k) != KeySize {
				return nil, fmt.Errorf("secrets: key must decode to %d bytes, got %d", KeySize, len(k))
			}
			return k, nil
		}
	}
	return nil, errors.New("secrets: key must be base64 or hex encoded")
}

// GenerateKey returns a new random key, base64 encoded.
func GenerateKey() (string, error) {
	k := make([]byte, KeySize)
	if _, err := io.ReadFull(rand.Reader, k); err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(k), nil
}

// KeySource describes where the primary key came from (for logs and the
// console; never the key itself).
type KeySource string

const (
	SourceNone      KeySource = "none"
	SourceEnv       KeySource = "env"
	SourceFile      KeySource = "file"
	SourceGenerated KeySource = "generated"
)

// Options say where to find keys.
type Options struct {
	// Key is the primary key value (usually from the environment).
	Key string
	// KeyFile holds the primary key when Key is empty.
	KeyFile string
	// Generate creates KeyFile with a fresh random key (mode 0600) when
	// neither Key nor an existing KeyFile is available.
	Generate bool
	// Previous are older keys still accepted for opening.
	Previous []string
}

// Load builds a box from opts. Without any key and without Generate it
// returns a nil box and SourceNone: the installation runs, but storing
// secrets is refused with ErrNoKey.
func Load(opts Options) (*Box, KeySource, error) {
	var prev [][]byte
	for i, p := range opts.Previous {
		if strings.TrimSpace(p) == "" {
			continue
		}
		k, err := ParseKey(p)
		if err != nil {
			return nil, SourceNone, fmt.Errorf("previous key %d: %w", i+1, err)
		}
		prev = append(prev, k)
	}
	var (
		raw    string
		source KeySource
	)
	switch {
	case strings.TrimSpace(opts.Key) != "":
		raw, source = opts.Key, SourceEnv
	case opts.KeyFile != "":
		b, err := os.ReadFile(opts.KeyFile)
		switch {
		case err == nil:
			raw, source = string(b), SourceFile
		case errors.Is(err, os.ErrNotExist) && opts.Generate:
			k, gerr := GenerateKey()
			if gerr != nil {
				return nil, SourceNone, gerr
			}
			if err := writeKeyFile(opts.KeyFile, k); err != nil {
				return nil, SourceNone, fmt.Errorf("secrets: create key file: %w", err)
			}
			raw, source = k, SourceGenerated
		case errors.Is(err, os.ErrNotExist):
			return nil, SourceNone, nil
		default:
			return nil, SourceNone, fmt.Errorf("secrets: read key file: %w", err)
		}
	default:
		return nil, SourceNone, nil
	}
	k, err := ParseKey(raw)
	if err != nil {
		return nil, SourceNone, err
	}
	b, err := NewBox(k, prev...)
	if err != nil {
		return nil, SourceNone, err
	}
	return b, source, nil
}

// writeKeyFile creates path exclusively with owner-only permissions, so a
// concurrent start never overwrites a key another instance just wrote.
func writeKeyFile(path, key string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.WriteString(key + "\n"); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// Redact replaces every occurrence of the given secret values in s. Error
// messages from remote services sometimes echo the credential they
// rejected; anything shown to operators, logged, or stored goes through it.
func Redact(s string, values ...string) string {
	for _, v := range values {
		v = strings.TrimSpace(v)
		if len(v) < 4 {
			continue
		}
		s = strings.ReplaceAll(s, v, "[redacted]")
	}
	return s
}
