package keycloak

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"knowledge-agent/internal/logger"
)

// JWTClaims represents the claims extracted from a Keycloak JWT
type JWTClaims struct {
	Email         string   // User's email address
	Groups        []string // User's groups (from "groups" claim or configured path)
	PreferredUser string   // Preferred username
	Subject       string   // Subject (user ID in Keycloak)
}

// GroupsClaimPath specifies where to find groups in the JWT
// Common paths:
// - "groups" - direct groups claim (requires Keycloak mapper)
// - "realm_access.roles" - realm roles
// - "resource_access.<client>.roles" - client-specific roles
type GroupsClaimPath string

const (
	GroupsClaimGroups      GroupsClaimPath = "groups"             // Direct groups claim
	GroupsClaimRealmRoles  GroupsClaimPath = "realm_access.roles" // Realm roles
	GroupsClaimDefault     GroupsClaimPath = "groups"             // Default path
)

// ParseJWTClaims extracts claims from a JWT token without validating the signature.
// This is safe when the JWT has already been validated by Keycloak or an API Gateway.
// groupsClaimPath specifies where to find groups in the token (e.g., "groups", "realm_access.roles")
func ParseJWTClaims(token string, groupsClaimPath string) (*JWTClaims, error) {
	log := logger.Get()

	// Use default path if not specified
	if groupsClaimPath == "" {
		groupsClaimPath = string(GroupsClaimDefault)
	}

	// JWT format: header.payload.signature
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return nil, fmt.Errorf("invalid JWT format: expected 3 parts, got %d", len(parts))
	}

	// Decode payload (second part)
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		// Try standard base64 if URL encoding fails
		payload, err = base64.StdEncoding.DecodeString(parts[1])
		if err != nil {
			return nil, fmt.Errorf("failed to decode JWT payload: %w", err)
		}
	}

	// Parse JSON payload
	var claims map[string]any
	if err := json.Unmarshal(payload, &claims); err != nil {
		return nil, fmt.Errorf("failed to parse JWT claims: %w", err)
	}

	result := &JWTClaims{}

	// Extract email
	if email, ok := claims["email"].(string); ok {
		result.Email = email
	}

	// Extract preferred_username
	if preferredUser, ok := claims["preferred_username"].(string); ok {
		result.PreferredUser = preferredUser
	}

	// Extract subject
	if sub, ok := claims["sub"].(string); ok {
		result.Subject = sub
	}

	// Extract groups from the specified path
	result.Groups = extractGroupsFromPath(claims, groupsClaimPath)

	log.Debugw("JWT claims parsed",
		"email", result.Email,
		"preferred_user", result.PreferredUser,
		"groups_count", len(result.Groups),
		"groups_path", groupsClaimPath,
	)

	return result, nil
}

// extractGroupsFromPath extracts groups from a nested path in the claims
// Supports paths like "groups", "realm_access.roles", "resource_access.client.roles"
func extractGroupsFromPath(claims map[string]any, path string) []string {
	parts := strings.Split(path, ".")

	// Navigate to the nested value
	current := any(claims)
	for _, part := range parts {
		if m, ok := current.(map[string]any); ok {
			current = m[part]
		} else {
			return nil
		}
	}

	// Extract string array from the final value
	return extractStringArray(current)
}

// extractStringArray converts an interface{} to []string
func extractStringArray(v any) []string {
	if v == nil {
		return nil
	}

	switch arr := v.(type) {
	case []any:
		result := make([]string, 0, len(arr))
		for _, item := range arr {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	case []string:
		return arr
	default:
		return nil
	}
}

// ExtractBearerToken extracts the token from an "Authorization: Bearer <token>" header value
func ExtractBearerToken(authHeader string) string {
	if authHeader == "" {
		return ""
	}

	// Check for "Bearer " prefix (case-insensitive)
	const bearerPrefix = "Bearer "
	if len(authHeader) > len(bearerPrefix) && strings.EqualFold(authHeader[:len(bearerPrefix)], bearerPrefix) {
		return authHeader[len(bearerPrefix):]
	}

	return ""
}

// HasGroup checks if a user has a specific group
func (c *JWTClaims) HasGroup(group string) bool {
	for _, g := range c.Groups {
		if g == group {
			return true
		}
	}
	return false
}

// HasAnyGroup checks if a user has any of the specified groups
func (c *JWTClaims) HasAnyGroup(groups []string) bool {
	for _, required := range groups {
		if c.HasGroup(required) {
			return true
		}
	}
	return false
}
