// Copyright (C) 2024 and up by Alexander Pevzner (pzz@apevzner.com)
// See LICENSE for license terms and conditions
//
// Adapted from github.com/OpenPrinting/go-mfp
// Original author: Alexander Pevzner
// License: BSD 2-Clause
//
// Package main provides the ipp-registrations-validate tool, which performs
// strict validation of the IANA IPP registrations database (xml).
package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/OpenPrinting/go-mfp/argv"
	"github.com/OpenPrinting/go-mfp/util/xmldoc"
)

// Command describes the command-line interface for the validator.
// It defines the available flags (-i for input, -e for errata) and
// specifies the handler function.
var Command = argv.Command{
	Name: "ipp-registrations-validate",
	Help: "Strict validation tool for IANA IPP registrations database",
	Options: []argv.Option{
		argv.Option{
			Name:     "-i",
			Aliases:  []string{"--input"},
			Help:     "IANA ipp-registrations.xml input file",
			HelpArg:  "file",
			Required: true,
			Validate: argv.ValidateAny,
			Complete: argv.CompleteOSPath,
		},
		argv.Option{
			Name:     "-e",
			Aliases:  []string{"--errata"},
			Help:     "Errata XML file (replaces or adds to the IANA definitions)",
			HelpArg:  "file",
			Validate: argv.ValidateAny,
			Complete: argv.CompleteOSPath,
		},
		argv.HelpOption,
	},
	Handler: commandHandler,
}

// fileXML holds a filename and its parsed XML DOM.
type fileXML struct {
	name string
	xml  xmldoc.Element
}

// commandHandler is the entry point for the command logic. It manages
// the loading of XML files, populates the registration database,
// and reports any structural or syntax errors found.
func commandHandler(ctx context.Context, inv *argv.Invocation) error {
	// Create the in-memory registration database
	db := NewRegDB()

	var input, errata []fileXML

	// 1. Load errata files. Errata are processed separately because they
	// can preemptively mark certain IANA attributes to be skipped or
	// overwritten.
	for _, file := range inv.Values("-e") {
		xml, err := XMLLoad(file)
		if err != nil {
			return err
		}

		err = db.Load(file, xml, true)
		if err != nil {
			return err
		}
	}

	// 2. Load the main IANA input files.
	for _, file := range inv.Values("-i") {
		xml, err := XMLLoad(file)
		if err != nil {
			return err
		}

		input = append(input, fileXML{file, xml})
	}

	// 3. Process errata into the database first.
	for _, f := range errata {
		err := db.Load(f.name, f.xml, true)
		if err != nil {
			return err
		}
	}

	// 4. Process main input files.
	for _, f := range input {
		err := db.Load(f.name, f.xml, false)
		if err != nil {
			return err
		}
	}

	// 5. Finalize the database. This phase resolves attribute links/borrowing,
	// handles names with suffixes (e.g., "(extension)"), and identifies
	// empty collections.
	if err := db.Finalize(); err != nil {
		return err
	}

	// 6. Validate Enum and Keyword actual registered values
	for _, f := range input {
		db.ValidateValues(f.name, f.xml)
	}

	// 7. Report collected errors. Each error is printed in file:line: message format.
	if len(db.Errors) != 0 {
		for _, err := range db.Errors {
			s := strings.TrimRight(err.Error(), "\n")
			fmt.Println(s)
		}

		return fmt.Errorf("%d errors encountered", len(db.Errors))
	}

	fmt.Println("Validation successful. No errors found.")
	return nil
}

// main launches the Command using the go-mfp/argv framework.
func main() {
	Command.Main(context.Background())
}
