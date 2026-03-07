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

	// Load errata files
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

	// Load input file
	for _, file := range inv.Values("-i") {
		xml, err := XMLLoad(file)
		if err != nil {
			return err
		}

		err = db.Load(file, xml, false)
		if err != nil {
			return err
		}
	}

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
