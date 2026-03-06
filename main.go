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
// The main function

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/OpenPrinting/go-mfp/argv"
	"github.com/OpenPrinting/go-mfp/util/xmldoc"
)

// Command describes command options
var Command = argv.Command{
	Name: "ipp-registrations-validate",
	Help: "Strict validation tool for IANA IPP registrations database",
	Options: []argv.Option{
		argv.Option{
			Name:     "-i",
			Aliases:  []string{"--input"},
			Help:     "input file",
			HelpArg:  "file",
			Required: true,
			Validate: argv.ValidateAny,
			Complete: argv.CompleteOSPath,
		},
		argv.Option{
			Name:     "-e",
			Aliases:  []string{"--errata"},
			Help:     "errata file (takes precedence over input)",
			HelpArg:  "file",
			Validate: argv.ValidateAny,
			Complete: argv.CompleteOSPath,
		},
		argv.HelpOption,
	},
	Handler: commandHandler,
}

// commandHandler executes the command
func commandHandler(ctx context.Context, inv *argv.Invocation) error {
	// Create the database
	db := NewRegDB()

	type fileXML struct {
		name string
		xml  xmldoc.Element
	}
	var input, errata []fileXML

	// Load errata files first. We keep these separate because errata
	// entries take precedence and will overwrite standard definitions.
	for _, file := range inv.Values("-e") {
		xml, err := XMLLoad(file)
		if err != nil {
			return err
		}

		errata = append(errata, fileXML{file, xml})
	}

	// Load input file
	for _, file := range inv.Values("-i") {
		xml, err := XMLLoad(file)
		if err != nil {
			return err
		}

		input = append(input, fileXML{file, xml})
	}

	// Process loaded files into the in-memory database.
	// We load errata first so the database knows which attributes
	// have overrides before it parses the standard IANA dataset.
	for _, f := range errata {
		err := db.Load(f.name, f.xml, true)
		if err != nil {
			return err
		}
	}

	for _, f := range input {
		err := db.Load(f.name, f.xml, false)
		if err != nil {
			return err
		}
	}

	// Finalize does all the cross-referencing: it expands errata elements,
	// links attributes that borrow from other collections, and checks for
	// empty collections that were just stubs.
	if err := db.Finalize(); err != nil {
		return err
	}

	if len(db.Errors) != 0 {
		for _, err := range db.Errors {
			s := strings.TrimRight(err.Error(), "\n")
			fmt.Println(s)
		}

		err := fmt.Errorf("%d errors encountered", len(db.Errors))
		fmt.Println(err)
		return err
	}

	fmt.Println("Validation successful. No errors found.")
	return nil
}

// The main function
func main() {
	Command.Main(context.Background())
}
