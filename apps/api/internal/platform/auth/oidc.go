package auth

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// Claims is the identity information extracted from a Casdoor access token.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	Roles         []string
}

// TokenVerifier validates bearer tokens issued by the identity provider.
type TokenVerifier interface {
	Verify(ctx context.Context, raw string) (Claims, error)
}

// OIDCVerifier verifies JWT access tokens against an OIDC issuer.
//
// Discovery is lazy and retried, so the API starts (and /readyz passes) even
// if Casdoor is still booting; only authenticated requests wait for it.
type OIDCVerifier struct {
	issuer     string
	jwksURL    string
	audiences  []string
	rolesClaim string
	client     *http.Client

	mu       sync.Mutex
	verifier *oidc.IDTokenVerifier
}

// NewOIDCVerifier configures a verifier. Tokens must carry at least one of
// audiences in their "aud" claim (web and mobile use different client ids).
// If jwksURL is empty the keys are found through OIDC discovery.
func NewOIDCVerifier(issuer, jwksURL string, audiences []string, rolesClaim string) *OIDCVerifier {
	return &OIDCVerifier{
		issuer:     issuer,
		jwksURL:    jwksURL,
		audiences:  audiences,
		rolesClaim: rolesClaim,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

func (v *OIDCVerifier) get() (*oidc.IDTokenVerifier, error) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if v.verifier != nil {
		return v.verifier, nil
	}
	// The provider keeps this context for later JWKS refreshes, so it must
	// not be tied to a single request.
	ctx := oidc.ClientContext(context.Background(), v.client)
	// Audience is checked in Verify against several allowed client ids.
	cfg := &oidc.Config{SkipClientIDCheck: true}
	if v.jwksURL != "" {
		v.verifier = oidc.NewVerifier(v.issuer, oidc.NewRemoteKeySet(ctx, v.jwksURL), cfg)
		return v.verifier, nil
	}
	p, err := oidc.NewProvider(ctx, v.issuer)
	if err != nil {
		return nil, fmt.Errorf("oidc discovery: %w", err)
	}
	v.verifier = p.Verifier(cfg)
	return v.verifier, nil
}

// Verify checks signature, issuer, expiry and audience, then extracts claims.
func (v *OIDCVerifier) Verify(ctx context.Context, raw string) (Claims, error) {
	verifier, err := v.get()
	if err != nil {
		return Claims{}, err
	}
	tok, err := verifier.Verify(ctx, raw)
	if err != nil {
		return Claims{}, fmt.Errorf("%w: %w", errInvalidToken, err)
	}
	if !slices.ContainsFunc(tok.Audience, func(a string) bool { return slices.Contains(v.audiences, a) }) {
		return Claims{}, fmt.Errorf("%w: audience not allowed", errInvalidToken)
	}
	var m map[string]json.RawMessage
	if err := tok.Claims(&m); err != nil {
		return Claims{}, fmt.Errorf("%w: %w", errInvalidToken, err)
	}
	return parseClaims(tok.Subject, m, v.rolesClaim), nil
}

// parseClaims tolerates the different shapes Casdoor emits depending on the
// application's token format (roles as strings or as role objects).
func parseClaims(sub string, m map[string]json.RawMessage, rolesClaim string) Claims {
	c := Claims{Subject: sub}
	str := func(k string) string {
		var s string
		_ = json.Unmarshal(m[k], &s)
		return s
	}
	boolean := func(k string) bool {
		var b bool
		_ = json.Unmarshal(m[k], &b)
		return b
	}
	c.Email = str("email")
	c.EmailVerified = boolean("email_verified") || boolean("emailVerified")
	for _, k := range []string{"displayName", "name", "preferred_username"} {
		if c.Name = str(k); c.Name != "" {
			break
		}
	}

	var items []json.RawMessage
	_ = json.Unmarshal(m[rolesClaim], &items)
	for _, it := range items {
		var s string
		if json.Unmarshal(it, &s) == nil && s != "" {
			c.Roles = append(c.Roles, s)
			continue
		}
		var obj struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(it, &obj) == nil && obj.Name != "" {
			c.Roles = append(c.Roles, obj.Name)
		}
	}
	return c
}
