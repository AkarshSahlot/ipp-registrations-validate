// MFP - Miulti-Function Printers and scanners toolkit
// IPP registrations to Go converter.
//
// Copyright (C) 2024 and up by Alexander Pevzner (pzz@apevzner.com)
// See LICENSE for license terms and conditions
//
// XML loader

package main

import (
	"os"
	"strings"

	"github.com/OpenPrinting/go-mfp/util/xmldoc"
)

// XMLLoad reads the XML file.
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

// xmlCleanup performs some post-load cleanup on the loaded
// XML document:
//
//  1. Our XML parser doesn't support XML files without namespace
//     prefixes, while IANA registration database is one of these
//     files. If namespace prefixes are missed, XML parser translates
//     them into "-:". This function removes these unneeded prefixes.
//  2. Element's Text is trimmed (just in case).
func xmlCleanup(root *xmldoc.Element) {
	root.Name, _ = strings.CutPrefix(root.Name, "-:")
	for i := range root.Children {
		chld := &root.Children[i]
		chld.Text = strings.TrimSpace(chld.Text)
		xmlCleanup(chld)
	}
}
