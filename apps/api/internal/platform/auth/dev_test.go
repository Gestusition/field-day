package auth

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDevVerifier(t *testing.T) {
	v := DevVerifier{AdminRole: "admin"}
	ctx := context.Background()

	c, err := v.Verify(ctx, "dev")
	require.NoError(t, err)
	assert.Equal(t, "dev", c.Subject)
	assert.Equal(t, []string{"admin"}, c.Roles, "the dev account is an admin")

	for _, bad := range []string{"", "DEV", "dev:x", "developer", "eyJhbGciOi.jwt.token"} {
		_, err := v.Verify(ctx, bad)
		assert.ErrorIs(t, err, errInvalidToken, bad)
	}
}
