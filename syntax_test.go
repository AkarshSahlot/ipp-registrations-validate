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
	"reflect"
	"testing"

	"github.com/OpenPrinting/goipp"
)

func TestParseSyntax(t *testing.T) {
	tests := []struct {
		name    string
		syntax  string
		want    Syntax
		wantErr bool
	}{
		{
			name:   "simple integer",
			syntax: "integer",
			want: Syntax{
				SetOf:      false,
				Collection: false,
				Tags:       []goipp.Tag{goipp.TagInteger},
				Min:        -2147483648,
				Max:        2147483647,
			},
			wantErr: false,
		},
		{
			name:   "integer with limits",
			syntax: "integer(-10:100)",
			want: Syntax{
				SetOf:      false,
				Collection: false,
				Tags:       []goipp.Tag{goipp.TagInteger},
				Min:        -10,
				Max:        100,
			},
			wantErr: false,
		},
		{
			name:   "1setOf keyword",
			syntax: "1setOf keyword",
			want: Syntax{
				SetOf:      true,
				Collection: false,
				Tags:       []goipp.Tag{goipp.TagKeyword},
				Min:        1,
				Max:        255,
			},
			wantErr: false,
		},
		{
			name:   "collection",
			syntax: "collection",
			want: Syntax{
				SetOf:      false,
				Collection: true,
				Tags:       []goipp.Tag{goipp.TagBeginCollection},
				Min:        -2147483648,
				Max:        2147483647,
			},
			wantErr: false,
		},
		{
			name:   "multiple types",
			syntax: "integer | name",
			want: Syntax{
				SetOf:      false,
				Collection: false,
				Tags:       []goipp.Tag{goipp.TagInteger, goipp.TagName},
				Min:        0,
				Max:        255,
			},
			wantErr: false,
		},
		{
			name:    "invalid syntax",
			syntax:  "invalid[",
			want:    Syntax{},
			wantErr: true,
		},
		{
			name:   "max limit",
			syntax: "integer(MAX)",
			want: Syntax{
				SetOf:      false,
				Collection: false,
				Tags:       []goipp.Tag{goipp.TagInteger},
				Min:        -2147483648,
				Max:        2147483647,
			},
			wantErr: false,
		},
		{
			name:   "type2 ignored",
			syntax: "type2 keyword",
			want: Syntax{
				SetOf:      false,
				Collection: false,
				Tags:       []goipp.Tag{goipp.TagKeyword},
				Min:        1,
				Max:        255,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseSyntax(tt.syntax)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseSyntax() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !tt.wantErr && !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseSyntax() = %+v, want %+v", got, tt.want)
			}
		})
	}
}
