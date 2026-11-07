package main

import (
	"crypto/subtle"
	"errors"
	"math/rand"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
)

func RandomString(length int) string {
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	chars := "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	result := make([]byte, length)
	for i := 0; i < length; i++ {
		result[i] = chars[r.Intn(len(chars))]
	}
	return string(result)
}

// normalizeName canonicalizes a satellite or target name. Names are
// case-insensitive because viper lower-cases all map keys when reading the
// config file, so every lookup must use the lower-case form.
func normalizeName(name string) string {
	return strings.ToLower(name)
}

// normalizeNames applies normalizeName to every element of a slice.
func normalizeNames(names []string) []string {
	if names == nil {
		return nil
	}
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = normalizeName(n)
	}
	return out
}

// nameParam returns the normalized satellite/target name from a URL parameter.
func nameParam(r *http.Request, key string) string {
	return normalizeName(chi.URLParam(r, key))
}

// ValidateIdentifier validates that an identifier (satellite name, target name, etc.)
// is not empty, within reasonable length, and contains only allowed characters.
// Allowed characters: alphanumeric, hyphen, underscore, dot
// Maximum length: 255 characters
func ValidateIdentifier(identifier string, identifierType string) error {
	if identifier == "" {
		return errors.New(identifierType + " cannot be empty")
	}

	if len(identifier) > 255 {
		return errors.New(identifierType + " exceeds maximum length of 255 characters")
	}

	// Allow alphanumeric characters, hyphens, underscores, and dots
	// Must not start or end with special characters
	validPattern := regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*[a-zA-Z0-9]$|^[a-zA-Z0-9]$`)
	if !validPattern.MatchString(identifier) {
		return errors.New(identifierType + " contains invalid characters or format. Only alphanumeric, hyphen, underscore, and dot are allowed")
	}

	return nil
}

// SecureCompareStrings performs a constant-time comparison of two strings.
// This function is resistant to timing attacks and should be used for comparing
// sensitive values like secrets, tokens, and passwords.
// Returns true if the strings are equal, false otherwise.
func SecureCompareStrings(a, b string) bool {
	// Convert strings to byte slices for constant-time comparison
	// subtle.ConstantTimeCompare returns 1 if equal, 0 if not equal
	return subtle.ConstantTimeCompare([]byte(a), []byte(b)) == 1
}

// CheckSecret verifies a client-provided secret against the configured one.
// Unlike SecureCompareStrings it fails closed: an unset (empty) configured
// secret or an empty provided secret never authenticates.
func CheckSecret(provided, expected string) bool {
	if expected == "" || provided == "" {
		return false
	}
	return SecureCompareStrings(provided, expected)
}
