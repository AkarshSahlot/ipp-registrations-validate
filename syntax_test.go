// MFP - Miulti-Function Printers and scanners toolkit
// IPP registrations to Go converter.
//
// Copyright (C) 2024 and up by Alexander Pevzner (pzz@apevzner.com)
// See LICENSE for license terms and conditions
//
// Adapted from github.com/OpenPrinting/go-mfp
// Original author: Alexander Pevzner
// License: BSD 2-Clause
//
// Attribute syntax tests

package main

import (
	"testing"
)

func TestParseSyntax(t *testing.T) {
	tests := []struct {
		name    string
		syntax  string
		wantErr bool
		errMsg  string
	}{
		// Valid cases
		{
			name:    "simple keyword",
			syntax:  "keyword",
			wantErr: false,
		},
		{
			name:    "integer with bounds",
			syntax:  "integer(0:MAX)",
			wantErr: false,
		},
		{
			name:    "collection union",
			syntax:  "collection | no-value",
			wantErr: false,
		},
		{
			name:    "1setOf with bounds",
			syntax:  "1setOf (integer(MIN:MAX))",
			wantErr: false,
		},
		{
			name:    "1setOf collection union",
			syntax:  "1setOf collection | no-value",
			wantErr: false,
		},
		{
			name:    "type2 keyword",
			syntax:  "1setOf type2 keyword | name(MAX)",
			wantErr: false,
		},

		// Invalid / Edge cases
		{
			name:    "empty syntax",
			syntax:  "",
			wantErr: false, // ParseSyntax handles empty string successfully returning default Syntax{}
		},
		{
			name:    "duplicate 1setOf",
			syntax:  "1setOf 1setOf keyword",
			wantErr: true,
			errMsg:  "duplicate 1setOf keyword",
		},
		{
			name:    "unnecessary apostrophe",
			syntax:  "keyword '",
			wantErr: true,
			errMsg:  "unnecessary apostrophe in syntax",
		},
		{
			name:    "invalid token",
			syntax:  "unknown-token",
			wantErr: true,
			errMsg:  "invalid token",
		},
		{
			name:    "invalid limit",
			syntax:  "integer(invalid:100)",
			wantErr: true,
			errMsg:  "invalid limit",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseSyntax(tt.syntax)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSyntax() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr && err != nil && tt.errMsg != "" {
				if !contains(err.Error(), tt.errMsg) {
					t.Errorf("ParseSyntax() error = %v, expected to contain %v", err, tt.errMsg)
				}
			}
		})
	}
}

// Helper to check if string contains substring, avoiding imports conflict if test fails
func contains(s, substr string) bool {
	// Simple strings.Contains implementation avoiding standard library import overhead in tests
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
