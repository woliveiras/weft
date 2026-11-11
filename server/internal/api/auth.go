package api

import (
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/woliveiras/weft/server/internal/config"
)

// Identity is the authenticated caller.
type Identity struct {
	Name string      `json:"name"`
	Role config.Role `json:"role"`
}

type ctxKey struct{}

func identityFrom(ctx context.Context) Identity {
	id, _ := ctx.Value(ctxKey{}).(Identity)
	return id
}

// Authenticator validates bearer credentials: static tokens (compared by
// SHA-256 digest in constant time, so token length and content do not leak
// through timing) and, optionally, OIDC JWTs (RS256/ES256 via JWKS).
type Authenticator struct {
	disabled bool
	tokens   map[[32]byte]Identity
	oidc     *oidcVerifier
}

func NewAuthenticator(c config.Auth) (*Authenticator, error) {
	a := &Authenticator{disabled: c.Disabled, tokens: map[[32]byte]Identity{}}
	for _, t := range c.Tokens {
		v, err := t.Resolve()
		if err != nil {
			return nil, err
		}
		if len(v) < 12 {
			return nil, fmt.Errorf("token %q is too short (min 12 chars)", t.Name)
		}
		a.tokens[sha256.Sum256([]byte(v))] = Identity{Name: t.Name, Role: t.Role}
	}
	if c.OIDC != nil {
		a.oidc = &oidcVerifier{cfg: *c.OIDC, keys: map[string]crypto.PublicKey{}}
	}
	return a, nil
}

func (a *Authenticator) Authenticate(r *http.Request) (Identity, error) {
	if a.disabled {
		return Identity{Name: "anonymous", Role: config.RoleAdmin}, nil
	}
	tok := ""
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		tok = strings.TrimSpace(h[7:])
	} else if q := r.URL.Query().Get("access_token"); q != "" && r.Method == http.MethodGet {
		// Only for GET (EventSource cannot set headers); never for writes.
		tok = q
	}
	if tok == "" {
		return Identity{}, errors.New("missing bearer token")
	}
	sum := sha256.Sum256([]byte(tok))
	for k, id := range a.tokens {
		if subtle.ConstantTimeCompare(k[:], sum[:]) == 1 {
			return id, nil
		}
	}
	if a.oidc != nil && strings.Count(tok, ".") == 2 {
		return a.oidc.verify(r.Context(), tok)
	}
	return Identity{}, errors.New("invalid token")
}

// --- minimal OIDC JWT verification -----------------------------------------

type oidcVerifier struct {
	cfg     config.OIDC
	mu      sync.Mutex
	keys    map[string]crypto.PublicKey
	fetched time.Time
}

func (o *oidcVerifier) verify(ctx context.Context, tok string) (Identity, error) {
	parts := strings.Split(tok, ".")
	hb, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return Identity{}, err
	}
	var hdr struct{ Alg, Kid string }
	if err := json.Unmarshal(hb, &hdr); err != nil {
		return Identity{}, err
	}
	key, err := o.key(ctx, hdr.Kid)
	if err != nil {
		return Identity{}, err
	}
	sig, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		return Identity{}, err
	}
	digest := sha256.Sum256([]byte(parts[0] + "." + parts[1]))
	switch hdr.Alg {
	case "RS256":
		pk, ok := key.(*rsa.PublicKey)
		if !ok || rsa.VerifyPKCS1v15(pk, crypto.SHA256, digest[:], sig) != nil {
			return Identity{}, errors.New("invalid token signature")
		}
	case "ES256":
		pk, ok := key.(*ecdsa.PublicKey)
		if !ok || len(sig) != 64 || !ecdsa.Verify(pk, digest[:], new(big.Int).SetBytes(sig[:32]), new(big.Int).SetBytes(sig[32:])) {
			return Identity{}, errors.New("invalid token signature")
		}
	default:
		return Identity{}, fmt.Errorf("unsupported alg %q", hdr.Alg)
	}
	pb, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return Identity{}, err
	}
	var claims map[string]any
	if err := json.Unmarshal(pb, &claims); err != nil {
		return Identity{}, err
	}
	if iss, _ := claims["iss"].(string); iss != o.cfg.Issuer {
		return Identity{}, errors.New("wrong issuer")
	}
	if exp, _ := claims["exp"].(float64); time.Now().Unix() > int64(exp) {
		return Identity{}, errors.New("token expired")
	}
	if o.cfg.Audience != "" && !audienceMatches(claims["aud"], o.cfg.Audience) {
		return Identity{}, errors.New("wrong audience")
	}
	roleClaim := o.cfg.RoleClaim
	if roleClaim == "" {
		roleClaim = "weft_role"
	}
	role := config.RoleViewer
	switch v := claims[roleClaim].(type) {
	case string:
		role = config.Role(v)
	case []any:
		for _, x := range v {
			if s, _ := x.(string); config.Role(s).Allows(role) {
				role = config.Role(s)
			}
		}
	}
	name, _ := claims["preferred_username"].(string)
	if name == "" {
		name, _ = claims["sub"].(string)
	}
	return Identity{Name: name, Role: role}, nil
}

func audienceMatches(aud any, want string) bool {
	switch v := aud.(type) {
	case string:
		return v == want
	case []any:
		for _, x := range v {
			if s, _ := x.(string); s == want {
				return true
			}
		}
	}
	return false
}

func (o *oidcVerifier) key(ctx context.Context, kid string) (crypto.PublicKey, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	if k := o.keys[kid]; k != nil && time.Since(o.fetched) < time.Hour {
		return k, nil
	}
	if time.Since(o.fetched) < 10*time.Second && len(o.keys) > 0 {
		return nil, errors.New("unknown signing key")
	}
	var disc struct {
		JWKSURI string `json:"jwks_uri"`
	}
	if err := getJSON(ctx, strings.TrimRight(o.cfg.Issuer, "/")+"/.well-known/openid-configuration", &disc); err != nil {
		return nil, err
	}
	var set struct {
		Keys []struct {
			Kid, Kty, N, E, Crv, X, Y string
		} `json:"keys"`
	}
	if err := getJSON(ctx, disc.JWKSURI, &set); err != nil {
		return nil, err
	}
	o.keys = map[string]crypto.PublicKey{}
	for _, k := range set.Keys {
		switch k.Kty {
		case "RSA":
			n, _ := base64.RawURLEncoding.DecodeString(k.N)
			e, _ := base64.RawURLEncoding.DecodeString(k.E)
			o.keys[k.Kid] = &rsa.PublicKey{N: new(big.Int).SetBytes(n), E: int(new(big.Int).SetBytes(e).Int64())}
		case "EC":
			if k.Crv != "P-256" {
				continue
			}
			x, _ := base64.RawURLEncoding.DecodeString(k.X)
			y, _ := base64.RawURLEncoding.DecodeString(k.Y)
			o.keys[k.Kid] = &ecdsa.PublicKey{Curve: elliptic.P256(), X: new(big.Int).SetBytes(x), Y: new(big.Int).SetBytes(y)}
		}
	}
	o.fetched = time.Now()
	if k := o.keys[kid]; k != nil {
		return k, nil
	}
	return nil, errors.New("unknown signing key")
}

func getJSON(ctx context.Context, url string, v any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	c := &http.Client{Timeout: 10 * time.Second}
	resp, err := c.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return fmt.Errorf("GET %s: HTTP %d", url, resp.StatusCode)
	}
	return json.NewDecoder(resp.Body).Decode(v)
}
