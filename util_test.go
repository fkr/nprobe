package main

import (
	"testing"
)

func TestValidateIdentifier(t *testing.T) {
	tests := []struct {
		name           string
		identifier     string
		identifierType string
		expectError    bool
	}{
		// Valid cases
		{"valid alphanumeric", "satellite1", "satellite name", false},
		{"valid with hyphen", "my-satellite", "satellite name", false},
		{"valid with underscore", "my_satellite", "satellite name", false},
		{"valid with dot", "my.satellite", "satellite name", false},
		{"valid mixed", "sat-1_test.prod", "satellite name", false},
		{"valid single char", "a", "satellite name", false},
		{"valid number", "1", "satellite name", false},

		// Invalid cases - empty
		{"empty string", "", "satellite name", true},

		// Invalid cases - too long
		{"too long", "a" + string(make([]byte, 255)), "satellite name", true},

		// Invalid cases - invalid characters
		{"with spaces", "my satellite", "satellite name", true},
		{"with special chars", "sat@llite", "satellite name", true},
		{"with slash", "sat/lite", "satellite name", true},
		{"with backslash", "sat\\lite", "satellite name", true},
		{"with semicolon", "sat;lite", "satellite name", true},
		{"with ampersand", "sat&lite", "satellite name", true},
		{"with pipe", "sat|lite", "satellite name", true},

		// Invalid cases - starting/ending with special chars
		{"starting with hyphen", "-satellite", "satellite name", true},
		{"ending with hyphen", "satellite-", "satellite name", true},
		{"starting with underscore", "_satellite", "satellite name", true},
		{"ending with underscore", "satellite_", "satellite name", true},
		{"starting with dot", ".satellite", "satellite name", true},
		{"ending with dot", "satellite.", "satellite name", true},

		// Edge cases
		{"all numbers", "12345", "satellite name", false},
		{"alphanumeric uppercase", "SATELLITE1", "satellite name", false},
		{"mixed case", "SaTeLLiTe", "satellite name", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateIdentifier(tt.identifier, tt.identifierType)
			if tt.expectError && err == nil {
				t.Errorf("ValidateIdentifier(%q, %q) expected error but got none", tt.identifier, tt.identifierType)
			}
			if !tt.expectError && err != nil {
				t.Errorf("ValidateIdentifier(%q, %q) unexpected error: %v", tt.identifier, tt.identifierType, err)
			}
		})
	}
}

func TestSecureCompareStrings(t *testing.T) {
	tests := []struct {
		name     string
		a        string
		b        string
		expected bool
	}{
		// Equal strings - should return true
		{"equal empty strings", "", "", true},
		{"equal single char", "a", "a", true},
		{"equal strings", "secret123", "secret123", true},
		{"equal long strings", "this-is-a-very-long-secret-token-12345", "this-is-a-very-long-secret-token-12345", true},
		{"equal with special chars", "secret!@#$%", "secret!@#$%", true},

		// Different strings - should return false
		{"different strings", "secret123", "secret456", false},
		{"different length", "secret", "secret123", false},
		{"different case", "Secret", "secret", false},
		{"empty vs non-empty", "", "secret", false},
		{"non-empty vs empty", "secret", "", false},
		{"prefix match", "secret", "secret123", false},
		{"suffix match", "123secret", "secret", false},
		{"one char difference", "secret1", "secret2", false},

		// Timing attack scenarios - different lengths and positions
		{"different at start", "xsecret", "asecret", false},
		{"different at middle", "secxret", "secaret", false},
		{"different at end", "secretx", "secreta", false},
		{"mostly similar", "secrettoken123", "secrettoken456", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := SecureCompareStrings(tt.a, tt.b)
			if result != tt.expected {
				t.Errorf("SecureCompareStrings(%q, %q) = %v, want %v", tt.a, tt.b, result, tt.expected)
			}
		})
	}
}

// TestSecureCompareStringsSymmetry verifies that comparison is symmetric
func TestSecureCompareStringsSymmetry(t *testing.T) {
	testPairs := []struct {
		a string
		b string
	}{
		{"secret", "secret"},
		{"secret1", "secret2"},
		{"", ""},
		{"a", ""},
	}

	for _, pair := range testPairs {
		resultAB := SecureCompareStrings(pair.a, pair.b)
		resultBA := SecureCompareStrings(pair.b, pair.a)
		if resultAB != resultBA {
			t.Errorf("SecureCompareStrings is not symmetric: (%q, %q) = %v, but (%q, %q) = %v",
				pair.a, pair.b, resultAB, pair.b, pair.a, resultBA)
		}
	}
}
