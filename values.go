package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/OpenPrinting/go-mfp/util/generic"
	"github.com/OpenPrinting/go-mfp/util/xmldoc"
)

// ValidateValues checks the registries 4 (Keywords) and 6 (Enums) against the attributes
func (db *RegDB) ValidateValues(file string, xml xmldoc.Element) {
	knownTables := generic.NewSet[string]()
	reportedTables := generic.NewSet[string]()

	// 1. Build a map of PureName -> []*RegDBAttr for efficient lookup by bare name
	attrsByPureName := make(map[string][]*RegDBAttr)
	for _, attr := range db.AllAttrs {
		name := attr.PureName()
		attrsByPureName[name] = append(attrsByPureName[name], attr)
	}

	for _, registry := range xml.Children {
		id, ok := registry.AttrByName("id")
		if !ok || (id.Value != "ipp-registrations-4" && id.Value != "ipp-registrations-6") {
			continue
		}

		isEnum := id.Value == "ipp-registrations-6"

		for _, record := range registry.Children {
			if record.Name != "record" {
				continue
			}

			attrNode, attrOk := record.ChildByName("attribute")
			if !attrOk {
				continue
			}
			attrName := strings.TrimSpace(attrNode.Text)

			// Skip if the attribute name is a "<Any ...>" pattern
			if strings.HasPrefix(attrName, "<Any ") {
				continue
			}

			// Remove "(deprecated)", "(extension)" or other suffixes for matching against db
			cleanAttrName := attrName
			if idx := strings.IndexByte(attrName, '('); idx > 0 {
				cleanAttrName = attrName[:idx]
			}
			cleanAttrName = strings.TrimSpace(cleanAttrName)

			knownTables.Add(cleanAttrName)

			// CHECK 4: Reverse Connectivity (values -> attribute)
			// BUG 3 Fix: Search by bare name across all attributes
			if !reportedTables.Contains(cleanAttrName) {
				if _, found := attrsByPureName[cleanAttrName]; !found {
					db.Errors = append(db.Errors, fmt.Errorf("%s:%d: %s: values table references unknown attribute", file, attrNode.Line, attrName))
					reportedTables.Add(cleanAttrName)
				}
			}

			valueNode, valOk := record.ChildByName("value")
			if !valOk {
				// Record just defines the table, no value item
				continue
			}
			value := strings.TrimSpace(valueNode.Text)

			// Preprocess value to strip descriptive tags or trailing remarks
			if strings.HasPrefix(value, "<") && strings.HasSuffix(value, ">") {
				continue
			}

			// Some values have "(deprecated)" or "(obsolete)"
			cleanValue := value
			if idx := strings.IndexByte(value, '('); idx > 0 {
				cleanValue = strings.TrimSpace(value[:idx])
			}

			// Validate value format
			if isEnum {
				// CHECK 2: Enum Value Format
				if _, err := strconv.ParseInt(cleanValue, 0, 64); err != nil {
					db.Errors = append(db.Errors, fmt.Errorf("%s:%d: %s/%s: enum value is not a valid number", file, valueNode.Line, attrName, value))
				}
			} else {
				// CHECK 3: Keyword Value Format
				if !isValidKeyword(cleanValue) {
					db.Errors = append(db.Errors, fmt.Errorf("%s:%d: %s/%s: keyword value is not a valid keyword", file, valueNode.Line, attrName, value))
				}
			}
		}
	}

	// CHECK 1: Connectivity (attribute -> values)
	for _, attr := range db.AllAttrs {
		if attr.Syntax.Collection || attr.SyntaxString == "" {
			continue
		}

		lowerSyntax := strings.ToLower(attr.SyntaxString)

		// Check if it's an enum or keyword
		needsTable := strings.Contains(lowerSyntax, "keyword") || strings.Contains(lowerSyntax, "enum")

		// If the syntax explicitly says "type2" or "type3", it allows vendor extensions
		// which means a complete values table at IANA is not strictly required.
		// Similarly, if the syntax includes "name(" (e.g., "keyword | name(MAX)"),
		// the attribute accepts vendor-defined names alongside keywords, making it
		// inherently extensible without requiring a fixed IANA values table.
		if strings.Contains(lowerSyntax, "type2") || strings.Contains(lowerSyntax, "type3") ||
			strings.Contains(lowerSyntax, "name(") {
			needsTable = false
		}

		// Collection member attributes (those nested inside a parent collection)
		// typically have their keyword values defined by the collection's
		// specification rather than registered in the IANA keyword values registry.
		if len(attr.Parents) > 0 {
			needsTable = false
		}

		// Skip -supported attributes (often they just list other attributes/keywords)
		if strings.HasSuffix(attr.PureName(), "-supported") || strings.HasSuffix(attr.PureName(), "-mandatory") {
			needsTable = false
		}

		// Skip deprecated or obsolete attributes from being strictly verified for tables
		if strings.Contains(attr.Name, "(deprecated)") || strings.Contains(attr.Name, "(obsolete)") {
			needsTable = false
		}

		if needsTable {
			// BUG 1 & 2 Fix: Check bare name AND singular form
			name := attr.PureName()
			found := knownTables.Contains(name)
			if !found && strings.HasSuffix(name, "s") {
				singular := name[:len(name)-1]
				found = knownTables.Contains(singular)
			}

			if !found {
				db.Errors = append(db.Errors, fmt.Errorf("%s:%d: %s: enum/keyword attribute has no registered values table", attr.File, attr.Line, attr.Path()))
			}
		}
	}
}

// isValidKeyword checks if a keyword contains only letters (including A-Z sometimes found in IANA), digits, '.', '+', '_', and hyphens.
func isValidKeyword(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '.' || c == '_' || c == '+') {
			return false
		}
	}
	return true
}
