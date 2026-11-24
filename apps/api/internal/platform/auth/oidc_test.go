package auth

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
)

func raw(t *testing.T, v any) json.RawMessage {
	t.Helper()
	b, err := json.Marshal(v)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestParseClaimsCasdoorRoleObjects(t *testing.T) {
	m := map[string]json.RawMessage{
		"email":          raw(t, "ada@example.com"),
		"email_verified": raw(t, true),
		"displayName":    raw(t, "Ada"),
		"name":           raw(t, "ada"),
		"roles":          raw(t, []map[string]string{{"name": "admin", "owner": "fieldday"}, {"name": "member"}}),
	}
	c := parseClaims("sub-1", m, "roles")
	assert.Equal(t, "sub-1", c.Subject)
	assert.Equal(t, "ada@example.com", c.Email)
	assert.True(t, c.EmailVerified)
	assert.Equal(t, "Ada", c.Name)
	assert.Equal(t, []string{"admin", "member"}, c.Roles)
}

func TestParseClaimsStringRolesAndFallbacks(t *testing.T) {
	m := map[string]json.RawMessage{
		"preferred_username": raw(t, "grace"),
		"emailVerified":      raw(t, true),
		"groups":             raw(t, []string{"staff"}),
	}
	c := parseClaims("sub-2", m, "groups")
	assert.Equal(t, "grace", c.Name)
	assert.True(t, c.EmailVerified)
	assert.Equal(t, []string{"staff"}, c.Roles)
}

func TestParseClaimsMissingRoles(t *testing.T) {
	c := parseClaims("sub-3", map[string]json.RawMessage{}, "roles")
	assert.Empty(t, c.Roles)
	assert.False(t, c.EmailVerified)
}
