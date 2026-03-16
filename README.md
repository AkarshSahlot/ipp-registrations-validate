# ipp-registrations-validate

A strict validation tool for the IANA IPP (Internet Printing Protocol) registrations database.

## Overview

Unlike forgiving parsers that silently ignore structural issues, this tool actively validates ipp-registrations.xml and reports all errors in a single pass with exact file:line locations.

## What It Checks

**Syntax validation**
- Invalid tokens in syntax strings (e.g. stray `]`)
- Wrong range separators (e.g. `integer:0:MAX` instead of `integer(0:MAX)`)
- Unnecessary apostrophes in syntax definitions
- Duplicate `1setOf` keywords
  
**Collection integrity**
- Broken cross-references between collections
- Circular self-references
- Collections declared as collection type but with no member definitions

**Enum and keyword values**
- Every enum/keyword attribute must have a registered values table in the IANA database
- Enum values must be valid integers
- Keyword values must be valid keyword format
- Values tables must reference attributes that actually exist in the registry

## Usage

go build
./ipp-registrations-validate -i path/to/ipp-registrations.xml

All errors are printed to stdout in the format:
file:line: path: error message

The tool exits with a non-zero status if any errors are found.

## Current Results

Running against the current IANA database (ipp-registrations.xml, last updated 2025-10-31) produces 119 errors — all verified as genuine issues in the upstream database.

## Errata Support

Known issues can be suppressed using an errata file:

./ipp-registrations-validate -i ipp-registrations.xml -e path/to/errata.xml

The errata format follows the same convention used by OpenPrinting/go-mfp.

## Related

- [IANA IPP Registrations](https://www.iana.org/assignments/ipp-registrations/ipp-registrations.xml)
- [OpenPrinting go-mfp](https://github.com/OpenPrinting/go-mfp)
