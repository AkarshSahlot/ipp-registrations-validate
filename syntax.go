// Copyright (C) 2024 and up by Alexander Pevzner (pzz@apevzner.com)
// See LICENSE for license terms and conditions
//
// Adapted from github.com/OpenPrinting/go-mfp
// Original author: Alexander Pevzner
// License: BSD 2-Clause
//
// Package main provides the ipp-registrations-validate tool.
// This file implements a parser for IPP attribute syntax strings.
package main

import (
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"
	"unicode"

	"github.com/OpenPrinting/go-mfp/util/generic"
	"github.com/OpenPrinting/goipp"
)

// Syntax represents the parsed properties of an IPP attribute's syntax.
// Reference examples of strings this parses:
//   - "integer"
//   - "integer(0:MAX)"
//   - "collection | no-value"
//   - "1setOf (integer(MIN:MAX))"
//   - "1setOf collection | no-value"
type Syntax struct {
	SetOf      bool        // SetOf is true if the attribute allows multiple values (1setOf keyword).
	Collection bool        // Collection is true if the attribute is a collection (begins with TagBeginCollection).
	Tags       []goipp.Tag // Tags contains the allowed IPP value tags (e.g., Integer, Keyword).
	Min, Max   int32       // Min and Max represent the numeric boundaries for the value (if applicable).
}

// MIN and MAX constants define the extreme ranges for 32-bit signed integers.
const (
	MIN = math.MinInt32
	MAX = math.MaxInt32
)

// Internal token types used during parsing.
type tok1setOF struct{} // tok1setOF represents the "1setOf" keyword.
type tokValue struct {  // tokValue represents a basic type (e.g., "integer") with its optional limits.
	tags     []goipp.Tag
	min, max int32
}

// ParseSyntax parses a raw IANA syntax string into a Syntax structure.
// It handles tokenization, limit extraction, and normalization of IPP tags.
func ParseSyntax(s string) (syntax Syntax, err error) {
	// First, convert the string into a slice of internal token types.
	tokens, err := syntax.decodeTokens(s)
	if err != nil {
		return
	}

	// Initialize with default wide limits.
	syntax.Min = MIN
	syntax.Max = MAX

	// Process the tokens to set flags and collect tags.
	for _, tok := range tokens {
		switch tok := tok.(type) {
		case tok1setOF:
			syntax.SetOf = true

		case tokValue:
			syntax.Tags = append(syntax.Tags, tok.tags...)
			// If multiple types are present (e.g., "integer | name"),
			// we intersect the limits.
			syntax.Min = generic.Max(syntax.Min, tok.min)
			syntax.Max = generic.Min(syntax.Max, tok.max)
		}
	}

	// Sort and deduplicate tags to ensure functional equality checks work consistently.
	syntax.sortTags()

	// Final pass: apply tag-specific default limits from the goipp library.
	for _, tag := range syntax.Tags {
		min, max := tag.Limits()
		syntax.Min = generic.Max(syntax.Min, min)
		syntax.Max = generic.Min(syntax.Max, max)

		if tag == goipp.TagBeginCollection {
			syntax.Collection = true
		}
	}

	return
}

// Equal returns true if two Syntax objects are functionally equivalent.
func (syntax Syntax) Equal(syntax2 Syntax) bool {
	return reflect.DeepEqual(syntax, syntax2)
}

// FormatMin returns the minimum limit as a string ("MIN" or decimal).
func (syntax Syntax) FormatMin() string {
	if syntax.Min == MIN {
		return "MIN"
	}
	return strconv.FormatInt(int64(syntax.Min), 10)
}

// FormatMax returns the maximum limit as a string ("MAX" or decimal).
func (syntax Syntax) FormatMax() string {
	if syntax.Max == MAX {
		return "MAX"
	}
	return strconv.FormatInt(int64(syntax.Max), 10)
}

// decodeTokens converts the raw string into a structured slice of tokens.
// It handles keyword filtering (ignoring "type2", etc.) and limit parsing.
func (syntax Syntax) decodeTokens(s string) ([]any, error) {
	strtok := syntax.tokenize(s)
	tokens := make([]any, 0, len(strtok))
	count1setOf := 0

	for i := 0; i < len(strtok); i++ {
		tok := strings.ToLower(strtok[i])
		switch tok {
		case "1setof":
			count1setOf++
			if count1setOf > 1 {
				return nil, fmt.Errorf("duplicate 1setOf keyword")
			}
			tokens = append(tokens, tok1setOF{})

		case "type1", "type2", "type3":
			// These are standard IPP "extensibility" keywords; they don't affect validation logic.
		case "(", ")", "|":
			// Structural punctuation.
		case "'":
			// Sometimes IANA puts unnecessary apostrophes (e.g., name('MAX')).
			return nil, fmt.Errorf("unnecessary apostrophe in syntax")

		default:
			// Resolve the string token to one or more IPP tags (e.g., "name" -> [TagName, TagNameLang]).
			tags := tags[tok]
			if tags == nil {
				return nil, fmt.Errorf("%q: invalid token %q", s, strtok[i])
			}

			// Check if the next tokens contain limits, e.g., "(0:255)".
			min, max, skip, err := syntax.decodeLimits(strtok[i+1:])
			if err != nil {
				return nil, fmt.Errorf("%q: %w", s, err)
			}

			tokens = append(tokens, tokValue{
				tags: tags,
				min:  min,
				max:  max,
			})
			i += skip
		}
	}

	return tokens, nil
}

// decodeLimits extracts (MIN:MAX) or (MAX) style boundaries.
func (syntax Syntax) decodeLimits(strtok []string) (min, max int32, consumed int, err error) {
	min, max = MIN, MAX

	switch {
	// Pattern: (MAX)
	case len(strtok) >= 3 && strtok[0] == "(" && strtok[2] == ")":
		max, err = syntax.decodeMinMax(strtok[1])
		if err == nil {
			consumed = 3
		}

	// Pattern: (MIN:MAX)
	case len(strtok) >= 5 && strtok[0] == "(" && strtok[2] == ":" && strtok[4] == ")":
		min, err = syntax.decodeMinMax(strtok[1])
		if err == nil {
			max, err = syntax.decodeMinMax(strtok[3])
		}
		if err == nil {
			consumed = 5
		}
	}

	return
}

// decodeMinMax converts a string boundary to a 32-bit integer,
// correctly interpreting "MIN" and "MAX" keywords.
func (syntax Syntax) decodeMinMax(s string) (v int32, err error) {
	switch strings.ToLower(s) {
	case "min":
		return MIN, nil
	case "max":
		return MAX, nil
	}

	var tmp int64
	tmp, err = strconv.ParseInt(s, 10, 32)
	if err != nil {
		return 0, fmt.Errorf("invalid limit %q", s)
	}

	return int32(tmp), nil
}

// tokenize splits the raw string into words, numbers, and punctuation.
func (syntax Syntax) tokenize(s string) []string {
	var strtok []string
	in := []byte(s)

	for len(in) != 0 {
		c := rune(in[0])
		switch {
		case unicode.IsLetter(c) || unicode.IsDigit(c):
			token := ""
			for len(in) > 0 && (unicode.IsLetter(rune(in[0])) || unicode.IsDigit(rune(in[0])) || in[0] == '-') {
				token += string(in[0])
				in = in[1:]
			}
			strtok = append(strtok, token)

		case c == '-':
			// Handle negative numbers.
			token := string(c)
			in = in[1:]
			for len(in) > 0 && unicode.IsDigit(rune(in[0])) {
				token += string(in[0])
				in = in[1:]
			}
			strtok = append(strtok, token)

		case unicode.IsSpace(c):
			in = in[1:]

		default:
			// Single-character punctuation.
			strtok = append(strtok, string(c))
			in = in[1:]
		}
	}

	return strtok
}

// sortTags normalizes the tag list: removes duplicates and applies a
// canonical sorting order for consistent comparison.
func (syntax *Syntax) sortTags() {
	tagSet := generic.NewSet[goipp.Tag]()
	for _, tag := range syntax.Tags {
		tagSet.Add(tag)
	}

	// Dedup Name/NameLang and Text/TextLang variants.
	if tagSet.Contains(goipp.TagName) || tagSet.Contains(goipp.TagNameLang) {
		tagSet.Add(goipp.TagName)
		tagSet.Del(goipp.TagNameLang)
	}
	if tagSet.Contains(goipp.TagText) || tagSet.Contains(goipp.TagTextLang) {
		tagSet.Add(goipp.TagText)
		tagSet.Del(goipp.TagTextLang)
	}

	syntax.Tags = syntax.Tags[:0]
	for _, tag := range tagsSortingOrder {
		if tagSet.Contains(tag) {
			syntax.Tags = append(syntax.Tags, tag)
		}
	}
}

// tagsSortingOrder ensures tags are always stored in a predictable sequence.
var tagsSortingOrder = []goipp.Tag{
	goipp.TagEnum,
	goipp.TagInteger,
	goipp.TagKeyword,
	goipp.TagName,
	goipp.TagNameLang,
	goipp.TagText,
	goipp.TagTextLang,
	goipp.TagBoolean,
	goipp.TagString,
	goipp.TagDateTime,
	goipp.TagResolution,
	goipp.TagRange,
	goipp.TagBeginCollection,
	goipp.TagReservedString,
	goipp.TagURI,
	goipp.TagURIScheme,
	goipp.TagCharset,
	goipp.TagLanguage,
	goipp.TagMimeType,
	goipp.TagUnsupportedValue,
	goipp.TagDefault,
	goipp.TagUnknown,
	goipp.TagNoValue,
	goipp.TagNotSettable,
	goipp.TagDeleteAttr,
	goipp.TagAdminDefine,
}

// tags lookup maps IANA strings to their goipp Tag equivalents.
var tags = map[string][]goipp.Tag{}

func init() {
	tags["collection"] = []goipp.Tag{goipp.TagBeginCollection}
	tags["name"] = []goipp.Tag{goipp.TagName, goipp.TagNameLang}
	tags["text"] = []goipp.Tag{goipp.TagText, goipp.TagTextLang}

	for tag := goipp.TagUnsupportedValue; tag < goipp.TagExtension; tag++ {
		switch tag {
		case goipp.TagBeginCollection, goipp.TagEndCollection, goipp.TagMemberName:
			// Handled explicitly.
		default:
			tags[strings.ToLower(tag.String())] = []goipp.Tag{tag}
		}
	}
}
