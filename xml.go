// Copyright (C) 2024 and up by Alexander Pevzner (pzz@apevzner.com)
// See LICENSE for license terms and conditions
//
// Adapted from github.com/OpenPrinting/go-mfp
// Original author: Alexander Pevzner
// License: BSD 2-Clause
//
// Package main provides the ipp-registrations-validate tool.
// This file handles loading and preprocessing of XML documents.
package main

import (
	"os"
	"strings"

	"github.com/OpenPrinting/go-mfp/util/xmldoc"
)

// XMLLoad reads and decodes an XML file into a generic Element tree.
// It uses the custom xmldoc parser because the IANA database is large
// and lacks a rigid schema that would easily map to fixed Go structs.
func XMLLoad(name string) (xmldoc.Element, error) {
	// Open the input file for reading.
	file, err := os.Open(name)
	if err != nil {
		return xmldoc.Element{}, err
	}
	defer file.Close()

	// Decode the file content into an xmldoc.Element tree.
	// The nil argument is for an optional logger.
	xml, err := xmldoc.Decode(nil, file)
	if err != nil {
		return xmldoc.Element{}, err
	}

	// Scrub the loaded XML to handle parser quirks and whitespace.
	xmlCleanup(&xml)

	return xml, nil
}

// xmlCleanup performs post-load normalization on the XML tree:
//
//  1. Namespace Stripping: The IANA database uses a default namespace
//     without a prefix. The xmldoc parser represents these with a "-:"
//     prefix (e.g., "-:record"). This function removes that prefix
//     for easier matching.
//  2. Whitespace Trimming: Leading/trailing whitespace in text content
//     is removed to ensure string comparisons are reliable.
func xmlCleanup(root *xmldoc.Element) {
	// Remove the "-:" prefix from the element name if present.
	root.Name, _ = strings.CutPrefix(root.Name, "-:")

	// Recursively clean up all child elements.
	for i := range root.Children {
		chld := &root.Children[i]
		chld.Text = strings.TrimSpace(chld.Text)
		xmlCleanup(chld)
	}
}
