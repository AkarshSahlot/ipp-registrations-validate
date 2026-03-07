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
// Unit tests for the attribute syntax parser.

package main

import (
	"testing"

	"github.com/OpenPrinting/goipp"
)

func TestParseSyntax(t *testing.T) {
	tests := []struct {
		input    string
		expected Syntax
	}{
		{
			input: "integer",
			expected: Syntax{
				Tags: []goipp.Tag{goipp.TagInteger},
				Min:  -2147483648,
				Max:  2147483647,
			},
		},
		{
			input: "integer (0:MAX)",
			expected: Syntax{
				Tags: []goipp.Tag{goipp.TagInteger},
				Min:  0,
				Max:  2147483647,
			},
		},
		{
			input: "1setOf (integer(MIN:MAX))",
			expected: Syntax{
				SetOf: true,
				Tags:  []goipp.Tag{goipp.TagInteger},
				Min:   -2147483648,
				Max:   2147483647,
			},
		},
		{
			input: "collection | no-value",
			expected: Syntax{
				Collection: true,
				Tags:       []goipp.Tag{goipp.TagBeginCollection, goipp.TagNoValue},
				Min:        -2147483648,
				Max:        2147483647,
			},
		},
		{
			input: "1setOf type2 keyword | name(MAX)",
			expected: Syntax{
				SetOf: true,
				Tags:  []goipp.Tag{goipp.TagKeyword, goipp.TagName},
				Min:   1,
				Max:   255,
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			got, err := ParseSyntax(tc.input)
			if err != nil {
				t.Fatalf("ParseSyntax(%q) failed: %v", tc.input, err)
			}

			if !got.Equal(tc.expected) {
				t.Errorf("ParseSyntax(%q) = %+v, want %+v", tc.input, got, tc.expected)
			}
		})
	}
}
