# ipp-registrations-validate

A strict validation tool for the IANA IPP (Internet Printing Protocol) registrations database.

## Overview
Unlike forgiving parsers that silently ignore structural issues, this tool actively validates `ipp-registrations.xml` and detects:
- Missing mandatory fields (e.g. `<collection>`, `<name>`, `<syntax>`).
- Broken collection references and unresolvable links (e.g. `<Any...>` references).
- Duplicate `1setOf` keywords and rogue apostrophes in syntax definitions.
- Collections missing member definitions.

It is designed to be run against the official IANA database prior to submitting patches to the IETF PWG.

## Usage

```bash
go run . -i path/to/ipp-registrations.xml
```

Errors are logged to `stdout`. If no formal errors are found, the tool exits cleanly.
