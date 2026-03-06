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
// XML loader

package main

import (
	"os"
	"strings"

	"github.com/OpenPrinting/go-mfp/util/xmldoc"
)

// XMLLoad reads the XML file using our custom xmldoc parser.
// We use a custom parser instead of Go's encoding/xml because the IANA
// database is huge and loosely structured; extracting it into a generic DOM
// element tree is much easier than defining hundreds of structs.
func XMLLoad(name string) (xmldoc.Element, error) {
	// Open input file
	file, err := os.Open(name)
	if err != nil {
		return xmldoc.Element{}, err
	}
	defer file.Close()

	// Decode to XML
	xml, err := xmldoc.Decode(nil, file)
	if err != nil {
		return xmldoc.Element{}, err
	}

	// Cleanup loaded XML
	xmlCleanup(&xml)

	return xml, nil
}

// xmlCleanup performs some post-load cleanup on the loaded XML document:
//
//  1. Our XML parser doesn't support XML files without namespace prefixes,
//     and the IANA registration database lacks them. The parser translates
//     them into "-:". This function strips those dummy prefixes out.
//  2. Text content is trimmed to avoid trailing/leading spaces messing up matches.
func xmlCleanup(root *xmldoc.Element) {
	root.Name, _ = strings.CutPrefix(root.Name, "-:")
	for i := range root.Children {
		chld := &root.Children[i]
		chld.Text = strings.TrimSpace(chld.Text)
		xmlCleanup(chld)
	}
}
