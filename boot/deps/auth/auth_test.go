package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	authapi "github.com/wippyai/runtime/api/auth"
	apierror "github.com/wippyai/runtime/api/error"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Config paths ---

func TestConfig_LocalPath(t *testing.T) {
	cfg := &Config{ProjectDir: "/project"}
	assert.Equal(t, filepath.Join("/project", LocalCredentialsDir, LocalCredentialsFile), cfg.LocalPath())
}

func TestConfig_LocalPath_Empty(t *testing.T) {
	cfg := &Config{}
	assert.Equal(t, "", cfg.LocalPath())
}

func TestConfig_GlobalPath(t *testing.T) {
	cfg := &Config{GlobalDir: "/global"}
	assert.Equal(t, filepath.Join("/global", GlobalCredentialsFile), cfg.GlobalPath())
}

func TestConfig_GlobalPath_Empty(t *testing.T) {
	cfg := &Config{GlobalDir: ""}
	assert.Equal(t, "", cfg.GlobalPath())
}

// --- ValidateTokenFormat ---

func TestValidateTokenFormat(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		wantErr string
	}{
		{"valid", "wpy_abcdefghijklmnopqr", ""},
		{"empty", "", "token is empty"},
		{"wrong prefix", "tok_abcdefghijklmnopqr", "must start with"},
		{"too short", "wpy_abc", "too short"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateTokenFormat(tt.token)
			if tt.wantErr == "" {
				assert.NoError(t, err)
			} else {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tt.wantErr)
			}
		})
	}
}

// --- parseOrgsResponse ---

func TestParseOrgsResponse_Valid(t *testing.T) {
	orgs := []orgResponse{
		{
			Org: struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				DisplayName string `json:"display_name"`
			}{
				ID: "org-1", Name: "acme", DisplayName: "Acme Corp",
			},
			Role: "admin",
		},
		{
			Org: struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				DisplayName string `json:"display_name"`
			}{
				ID: "org-2", Name: "beta", DisplayName: "Beta Inc",
			},
			Role: "member",
		},
	}

	body, err := json.Marshal(orgs)
	require.NoError(t, err)

	result, err := parseOrgsResponse(body)
	require.NoError(t, err)
	require.Len(t, result.Orgs, 2)

	assert.Equal(t, "org-1", result.Orgs[0].ID)
	assert.Equal(t, "acme", result.Orgs[0].Name)
	assert.Equal(t, "Acme Corp", result.Orgs[0].DisplayName)
	assert.Equal(t, "admin", result.Orgs[0].Role)

	assert.Equal(t, "org-2", result.Orgs[1].ID)
	assert.Equal(t, "member", result.Orgs[1].Role)
}

func TestParseOrgsResponse_Empty(t *testing.T) {
	result, err := parseOrgsResponse([]byte("[]"))
	require.NoError(t, err)
	assert.Empty(t, result.Orgs)
}

func TestParseOrgsResponse_InvalidJSON(t *testing.T) {
	_, err := parseOrgsResponse([]byte("{invalid"))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "parse response")
}

// --- StoredCredential.ToCredential ---

func TestStoredCredential_ToCredential(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	stored := &StoredCredential{
		Token:     "wpy_token123456789012",
		Registry:  "https://hub.example.com",
		UserID:    "user-1",
		Username:  "alice",
		Scope:     "publish",
		Orgs:      []string{"org-1", "org-2"},
		ExpiresAt: exp,
	}

	cred := stored.ToCredential()
	assert.Equal(t, stored.Token, cred.Token)
	assert.Equal(t, stored.Registry, cred.Registry)
	assert.Equal(t, stored.UserID, cred.UserID)
	assert.Equal(t, stored.Username, cred.Username)
	assert.Equal(t, authapi.ScopePublish, cred.Scope)
	assert.Equal(t, stored.Orgs, cred.Orgs)
	assert.Equal(t, exp, cred.ExpiresAt)
}

// --- FromCredential ---

func TestFromCredential(t *testing.T) {
	exp := time.Now().Add(time.Hour)
	cred := &authapi.Credential{
		Token:     "wpy_token123456789012",
		Registry:  "https://hub.example.com",
		UserID:    "user-1",
		Username:  "alice",
		Scope:     authapi.ScopeRead,
		Orgs:      []string{"org-1"},
		ExpiresAt: exp,
	}

	stored := FromCredential(cred)
	assert.Equal(t, cred.Token, stored.Token)
	assert.Equal(t, cred.Registry, stored.Registry)
	assert.Equal(t, cred.UserID, stored.UserID)
	assert.Equal(t, cred.Username, stored.Username)
	assert.Equal(t, "read", stored.Scope)
	assert.Equal(t, cred.Orgs, stored.Orgs)
	assert.Equal(t, exp, stored.ExpiresAt)
	assert.False(t, stored.CreatedAt.IsZero())
}

// --- Store ---

func newTestStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := t.TempDir()
	cfg := &Config{
		ProjectDir: dir,
		GlobalDir:  filepath.Join(dir, "global"),
	}
	return NewStore(cfg), dir
}

func TestStore_Set_Get(t *testing.T) {
	store, _ := newTestStore(t)

	cred := &authapi.Credential{
		Token:    "wpy_token123456789012",
		Registry: "https://hub.example.com",
		UserID:   "user-1",
		Username: "alice",
		Scope:    authapi.ScopePublish,
	}

	require.NoError(t, store.Set(cred, false))

	got, err := store.Get("https://hub.example.com")
	require.NoError(t, err)
	assert.Equal(t, cred.Token, got.Token)
	assert.Equal(t, cred.Registry, got.Registry)
	assert.Equal(t, cred.UserID, got.UserID)
}

func TestStore_Set_Global(t *testing.T) {
	store, dir := newTestStore(t)

	cred := &authapi.Credential{
		Token:    "wpy_token123456789012",
		Registry: "https://hub.example.com",
	}

	require.NoError(t, store.Set(cred, true))

	// verify file was created in global dir
	globalPath := filepath.Join(dir, "global", GlobalCredentialsFile)
	_, err := os.Stat(globalPath)
	assert.NoError(t, err)
}

func TestStore_Get_NotFound(t *testing.T) {
	store, _ := newTestStore(t)

	_, err := store.Get("https://hub.example.com")
	assert.Equal(t, authapi.ErrNotAuthenticated, err)
}

func TestStore_Remove(t *testing.T) {
	store, _ := newTestStore(t)

	cred := &authapi.Credential{
		Token:    "wpy_token123456789012",
		Registry: "https://hub.example.com",
	}

	require.NoError(t, store.Set(cred, false))
	require.NoError(t, store.Remove("https://hub.example.com", false))

	_, err := store.Get("https://hub.example.com")
	assert.Equal(t, authapi.ErrNotAuthenticated, err)
}

func TestStore_Remove_NonExistent(t *testing.T) {
	store, _ := newTestStore(t)
	assert.NoError(t, store.Remove("https://hub.example.com", false))
}

func TestStore_DefaultRegistry(t *testing.T) {
	store, _ := newTestStore(t)
	assert.Equal(t, DefaultRegistry, store.DefaultRegistry())
}

func TestStore_DefaultRegistry_FromGlobalConfig(t *testing.T) {
	store, _ := newTestStore(t)

	cred := &authapi.Credential{
		Token:    "wpy_token123456789012",
		Registry: "https://custom.example.com",
	}
	require.NoError(t, store.Set(cred, true))

	assert.Equal(t, "https://custom.example.com", store.DefaultRegistry())
}

func TestStore_Set_EmptyPath(t *testing.T) {
	cfg := &Config{} // both paths empty
	store := NewStore(cfg)

	cred := &authapi.Credential{
		Token:    "wpy_token123456789012",
		Registry: "https://hub.example.com",
	}

	err := store.Set(cred, false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "credentials path not configured")
}

func TestStore_Remove_ClearsDefault(t *testing.T) {
	store, _ := newTestStore(t)

	cred := &authapi.Credential{
		Token:    "wpy_token123456789012",
		Registry: "https://only.example.com",
	}
	require.NoError(t, store.Set(cred, false))
	require.NoError(t, store.Remove("https://only.example.com", false))

	// default should fall back to DefaultRegistry since no credentials remain
	assert.Equal(t, DefaultRegistry, store.DefaultRegistry())
}

// --- Error constructors ---

func TestNewTokenReadError(t *testing.T) {
	err := NewTokenReadError(assert.AnError)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "failed to read token")
}

func TestNewTokenEmptyError(t *testing.T) {
	err := NewTokenEmptyError()
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Contains(t, err.Error(), "token cannot be empty")
}

func TestNewTokenInvalidError(t *testing.T) {
	err := NewTokenInvalidError(assert.AnError)
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Contains(t, err.Error(), "invalid token format")
}

func TestNewValidationError(t *testing.T) {
	err := NewValidationError("hub.example.com", assert.AnError)
	assert.Equal(t, apierror.PermissionDenied, err.Kind())
	assert.Contains(t, err.Error(), "for hub.example.com")
}

func TestNewValidationError_EmptyRegistry(t *testing.T) {
	err := NewValidationError("", assert.AnError)
	assert.Equal(t, apierror.PermissionDenied, err.Kind())
	assert.Contains(t, err.Error(), "token validation failed")
}

func TestNewStoreError(t *testing.T) {
	err := NewStoreError("hub.example.com", assert.AnError)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "for hub.example.com")
}

func TestNewRemoveError(t *testing.T) {
	err := NewRemoveError("hub.example.com", assert.AnError)
	assert.Equal(t, apierror.Internal, err.Kind())
	assert.Contains(t, err.Error(), "for hub.example.com")
}

func TestNewClientError(t *testing.T) {
	err := NewClientError("hub.example.com", assert.AnError)
	assert.Equal(t, apierror.Invalid, err.Kind())
	assert.Contains(t, err.Error(), "for hub.example.com")
}

func TestNewClientError_EmptyRegistry(t *testing.T) {
	err := NewClientError("", assert.AnError)
	assert.Contains(t, err.Error(), "failed to create auth client")
}
