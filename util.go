package main

import (
	"crypto/subtle"
	"errors"
	"math/rand"
	"regexp"
	"time"
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
