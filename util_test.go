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
