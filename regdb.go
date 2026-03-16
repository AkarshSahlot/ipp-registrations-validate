// MFP - Miulti-Function Printers and scanners toolkit
// IPP registrations to Go converter.
//
// Copyright (C) 2024 and up by Alexander Pevzner (pzz@apevzner.com)
// See LICENSE for license terms and conditions
//
// Registrations database

package main

import (
	"bytes"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/OpenPrinting/go-mfp/util/generic"
	"github.com/OpenPrinting/go-mfp/util/xmldoc"
)

// RegDB represents the in-memory database of IANA IPP registrations.
// It stores collections, attributes, and their relationships, along with
// any errata (overrides) provided via external XML files.
type RegDB struct {
	Collections   map[string]map[string]*RegDBAttr // Collections maps top-level registry titles (e.g., "Job Template") to their attributes.
	AllAttrs      map[string]*RegDBAttr            // AllAttrs allows fast lookup of any attribute by its full path (e.g., "Collection/Attr/Member").
	AddUseMembers map[string][]RegDBLink           // AddUseMembers stores extra links injected via errata.
	Subst         map[string]string                // Subst handles manual path substitutions for ambiguous links.
	ErrataSkip    generic.Set[string]              // ErrataSkip contains paths that should be ignored entirely.
	Errata        map[string]*RegDBAttr            // Errata stores attribute definitions that replace the standard IANA ones.
	Errors        []error                          // Errors collects all validation issues found during loading and finalization.
	Borrowings    []RegDBBorrowing                 // Borrowings tracks which attributes inherit members from others.
	Exceptions    generic.Set[string]              // Exceptions lists members that should NOT be inherited during a borrowing.
}

// RegDBLink represents a symbolic link to another attribute.
// It includes source file and line information so that broken link
// errors can point to the exact location in the XML where the link was defined.
type RegDBLink struct {
	Path string // Path is the target attribute path (e.g., "Job Status/cover-front").
	File string // File is the XML filename containing the link.
	Line int    // Line is the line number in that file.
}

// RegDBBorrowing represents relations between collection attributes,
// where the RegDBBorrowing.From attribute borrows members from the
// RegDBBorrowing.To attribute.
type RegDBBorrowing struct {
	From string
	To   string
}

// RegDBAttr represents a single IPP attribute or member attribute.
type RegDBAttr struct {
	Name         string                // Name is the base name of the attribute (e.g., "media").
	Collection   string                // Collection is the top-level registry it belongs to.
	Parents      []string              // Parents lists the path of collection attributes leading to this one.
	SyntaxString string                // SyntaxString is the raw syntax text from the XML.
	Syntax       Syntax                // Syntax is the parsed, structured representation of the attribute's type.
	XRef         string                // XRef refers to the defining document (e.g., "RFC 8011").
	UseMembers   []RegDBLink           // UseMembers lists other attributes from which this one inherits members.
	Members      map[string]*RegDBAttr // Members holds child attributes for collection-type attributes.
	File         string                // File is the XML filename where this attribute was defined.
	Line         int                   // Line is the line number in that file.
}

// NewRegDB creates a new RegDB
func NewRegDB() *RegDB {
	return &RegDB{
		Collections:   make(map[string]map[string]*RegDBAttr),
		AllAttrs:      make(map[string]*RegDBAttr),
		AddUseMembers: make(map[string][]RegDBLink),
		Subst:         make(map[string]string),
		ErrataSkip:    generic.NewSet[string](),
		Errata:        make(map[string]*RegDBAttr),
		Exceptions:    generic.NewSet[string](),
	}
}

// Load parses an XML document and populates the RegDB.
// If errata is true, it treats the file as a set of overrides.
func (db *RegDB) Load(file string, xml xmldoc.Element, errata bool) error {
	for _, registry := range xml.Children {
		// The IANA XML is organized into various <registry> elements.
		if registry.Name != "registry" {
			continue
		}

		// Each registry contains multiple <record> elements.
		for _, record := range registry.Children {
			if record.Name != "record" {
				continue
			}

			// Individual records hold attribute definitions or links.
			err := db.loadRecord(file, record, errata)
			if err != nil {
				return err
			}
		}

		// Errata files may contain special directive elements like
		// <skip>, <subst>, or <use-members>.
		if errata {
			for _, chld := range registry.Children {
				var err error
				switch chld.Name {
				case "skip":
					err = db.loadSkip(file, chld)

				case "subst":
					err = db.loadSubst(file, chld)

				case "use-members":
					err = db.loadUseMembers(file, chld)
				}

				if err != nil {
					return err
				}
			}
		}
	}

	return nil
}

// Finalize must be called after all attribute files are loaded,
// using the [RegDB.Load] calls.
//
// It finalizes the database and performs all needed integrity
// checks.
func (db *RegDB) Finalize() error {
	db.expandErrata()
	db.handleSuffixes()
	db.resolveLinks()
	db.checkEmptyCollections()
	return nil
}

// loadRecord processes a single <record> element.
// It filters out non-attribute records (like Objects or Operations descriptions
// that lack a proper 'collection' or 'syntax' field) and creates either
// a formal attribute or a member-borrowing link.
func (db *RegDB) loadRecord(file string, record xmldoc.Element, errata bool) error {
	// Lookup fields we are interested in.
	// Only name and xref are strictly required for any record.
	// collection and syntax are required for attributes, but some
	// records (like objects) don't have them.
	collection := xmldoc.Lookup{Name: "collection"}
	name := xmldoc.Lookup{Name: "name", Required: true}
	syntax := xmldoc.Lookup{Name: "syntax"}
	xref := xmldoc.Lookup{Name: "xref", Required: true}
	member := xmldoc.Lookup{Name: "member_attribute"}
	submember := xmldoc.Lookup{Name: "sub-member_attribute"}

	missed := record.Lookup(&collection, &name, &syntax, &xref,
		&member, &submember)

	if missed != nil {
		return nil
	}

	// If collection or syntax is missing entirely, it's not an attribute record
	// we care about in this validator.
	if collection.Elem.Name == "" || syntax.Elem.Name == "" {
		return nil
	}

	// Flag empty or self-closing syntax tags. The IANA database uses
	// <syntax/> for link records, but we still want to track them.
	if syntax.Elem.Text == "" {
		from, to, err := db.newLink(
			file, record.Line,
			collection.Elem.Text,
			name.Elem.Text,
			member.Elem.Text,
			submember.Elem.Text,
		)
		if err == nil && !db.ErrataSkip.Contains(from) {
			attr := db.AllAttrs[from]
			if attr == nil {
				err = fmt.Errorf("%s:%d: %s->%s: broken source", file, record.Line, from, to)
			} else {
				attr.UseMembers = append(attr.UseMembers, RegDBLink{Path: to, File: file, Line: record.Line})
			}
		}

		return err
	}

	// Create attribute
	xreftext := xref.Elem.Text
	if atype, _ := xref.Elem.AttrByName("type"); atype.Value == "rfc" {
		adata, _ := xref.Elem.AttrByName("data")
		xreftext = adata.Value
	}

	attr, err := db.newRegDBAttr(
		file,
		record.Line,
		collection.Elem.Text,
		name.Elem.Text,
		member.Elem.Text,
		submember.Elem.Text,
		syntax.Elem.Text,
		xreftext,
	)

	if err != nil {
		db.Errors = append(db.Errors, err)
		return nil
	}

	// Add to the database
	if errata {
		err = db.addErrata(attr)
	} else {
		if !db.ErrataSkip.Contains(attr.Path()) {
			err = db.add(attr)
		}
	}

	return err
}

// loadUseMembers handles the "use-members" errata element.
// It allows an attribute (e.g., job-col-actual) to borrow members
// defined for another attribute (e.g., job-col).
func (db *RegDB) loadUseMembers(file string, link xmldoc.Element) error {
	// There are may be multiple <name>, <except> and <use> elements.
	// Gather them all.
	names := []string{}
	uses := []string{}
	exceptions := []string{}

	for _, chld := range link.Children {
		switch chld.Name {
		case "name":
			names = append(names, chld.Text)
		case "except":
			exceptions = append(exceptions, chld.Text)
		case "use":
			uses = append(uses, chld.Text)
		}
	}

	if len(names) == 0 {
		return fmt.Errorf("%s:%d: link: missed <name> elements", file, link.Line)
	}

	if len(uses) == 0 && len(exceptions) == 0 {
		return fmt.Errorf("%s:%d: link: missed <use> and <except> elements", file, link.Line)
	}

	// Now create links
	for _, name := range names {
		for _, use := range uses {
			err := db.newDirectLink(file, link.Line, name, use)
			if err != nil {
				return err
			}
		}
	}

	// And add exceptions
	for _, name := range names {
		for _, except := range exceptions {
			path := name + "/" + except
			if !db.Exceptions.TestAndAdd(path) {
				return fmt.Errorf("%s:%d: %q: duplicated exception", file, link.Line, path)
			}
		}
	}

	return nil
}

// loadSkip handles the "skip" errata element.
// It instructs the validator to ignore records with the specified names.
func (db *RegDB) loadSkip(file string, skip xmldoc.Element) error {
	_, ok := skip.ChildByName("name")
	if !ok {
		return fmt.Errorf("%s:%d: skip: missed <name> element", file, skip.Line)
	}

	// There are may be multiple "name" elements, roll over all of them
	for _, name := range skip.Children {
		if name.Name == "name" {
			if !db.ErrataSkip.TestAndAdd(name.Text) {
				return fmt.Errorf("%s:%d: skip %s: already added", file, name.Line, name.Text)
			}
		}
	}

	return nil
}

// loadSubst handles the "subst" errata element.
// It defines an explicit path for a link target to resolve ambiguity.
// (e.g., mapping "media-col" to "Job Template/media-col").
func (db *RegDB) loadSubst(file string, subst xmldoc.Element) error {
	name := xmldoc.Lookup{Name: "name", Required: true}
	path := xmldoc.Lookup{Name: "path", Required: true}

	missed := subst.Lookup(&name, &path)
	if missed != nil {
		return fmt.Errorf("%s:%d: subst %s: already added", file, subst.Line, name.Name)
	}

	return db.addSubst(file, subst.Line, name.Elem.Text, path.Elem.Text)
}

// addErrata adds attribute to the db.Errata
func (db *RegDB) addErrata(attr *RegDBAttr) error {
	path := attr.Path()

	if db.Errata[path] != nil {
		err := fmt.Errorf("%s: duplicated errata attribute", path)
		return err
	}

	db.Errata[path] = attr
	return nil
}

// add inserts a new attribute into the database's collection tree.
// It also builds the 'Parents' path and detects duplicate definitions.
func (db *RegDB) add(attr *RegDBAttr) error {
	// Make collection on demand
	collection := db.Collections[attr.Collection]
	if collection == nil {
		collection = make(map[string]*RegDBAttr)
		db.Collections[attr.Collection] = collection
	}

	// Check parents and build path
	var parent *RegDBAttr
	path := []string{attr.Collection}

	for i := range attr.Parents {
		path = append(path, attr.Parents[i])
		if parent != nil {
			parent = parent.Members[attr.Parents[i]]
		} else {
			parent = collection[attr.Parents[i]]
		}

		if parent == nil {
			err := fmt.Errorf("%s: no parent (%s)",
				attr.Path(), strings.Join(path, "/"))
			return err
		}
	}

	// Check for duplicates and save attribute
	pmap := collection
	if parent != nil {
		pmap = parent.Members
	}

	if attr2 := pmap[attr.Name]; attr2 != nil {
		err := fmt.Errorf("%s: conflicts with %s",
			attr.Path(), attr2.Path())
		return err
	}

	db.AllAttrs[attr.Path()] = attr
	pmap[attr.Name] = attr
	return nil
}

// CollectionNames returns names of attribute collections
func (db *RegDB) CollectionNames() []string {
	collections := make([]string, 0, len(db.Collections))
	for col := range db.Collections {
		collections = append(collections, col)
	}

	sort.Strings(collections)
	return collections
}

// Lookup returns attributes, associated with path.
// Path could look as follows:
//
//	Description/input-attributes-default - path to particular attribute
//	Job Template                         - path to entire collection
func (db *RegDB) Lookup(path string) map[string]*RegDBAttr {
	if i := strings.IndexByte(path, '/'); i < 0 {
		// Top-level collection requested
		return db.Collections[path]
	}

	// Particular attribute requested
	attr := db.AllAttrs[path]
	if attr != nil {
		return attr.Members
	}

	return nil
}

// resolveLinks is the second pass of validation. It transforms
// relative link paths (e.g., "media-col") into absolute ones
// and verifies that the link targets actually exist.
func (db *RegDB) resolveLinks() {
	for _, col := range db.CollectionNames() {
		attrs := db.Collections[col]
		db.resolveLinksRecursive(attrs)
	}
}

// resolveLinksRecursive does the actual work of resolving links
func (db *RegDB) resolveLinksRecursive(attrs map[string]*RegDBAttr) {
	// Process attrs in predictable order
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}
	sort.Strings(names)

	// Roll over all attributes in collection
	for _, name := range names {
		attr := attrs[name]
		db.resolveLink(attr)
	}

	// Visit all children recursively
	for _, name := range names {
		attr := attrs[name]
		db.resolveLinksRecursive(attr.Members)
	}
}

// resolveLink performs the path resolution for a single attribute.
// It handles member-borrowing (UseMembers) and substitution logic.
func (db *RegDB) resolveLink(attr *RegDBAttr) {
	// Lookup global db.AddUseMembers
	if added := db.AddUseMembers[attr.Path()]; added != nil {
		attr.UseMembers = append(attr.UseMembers, added...)
	}

	// Roll over all attr.UseMembers
	for i, link := range attr.UseMembers {
		use := link.Path

		// Lookup substitutions
		use = link.Path
		if subst, ok := db.Subst[use]; ok {
			use = subst
		}

		// Resolve link to the absolute path
		toplevel := false

		switch {
		case db.Collections[use] != nil:
			// Nothing to do: link refers the top-level collection.
			toplevel = true

		case strings.IndexByte(use, '/') >= 0:
			// Nothing to do: link is already absolute

		default:
			// Assume link points to the attr's neighbors
			splitpath := strings.Split(attr.Path(), "/")
			if len(splitpath) <= 0 {
				panic("splitpath empty")
			}

			splitpath[len(splitpath)-1] = use
			use = strings.Join(splitpath, "/")
		}

		// Validate link target
		if !toplevel {
			attr2 := db.AllAttrs[use]

			var err error
			switch {
			case attr2 == nil:
				err = fmt.Errorf("%s:%d: %s->%s: broken link",
					link.File, link.Line, attr.Path(), use)
				db.Errors = append(db.Errors, err)

			case attr2 == attr:
				err = fmt.Errorf("%s:%d: %s->%s: link to self",
					link.File, link.Line, attr.Path(), use)
				db.Errors = append(db.Errors, err)

			case len(attr2.Members) == 0:
				err = fmt.Errorf("%s:%d: %s->%s: link target enpty",
					link.File, link.Line, attr.Path(), use)
				db.Errors = append(db.Errors, err)
			}

			if err != nil {
				continue
			}
		}

		// Save resolved link
		attr.UseMembers[i].Path = use
		db.Borrowings = append(db.Borrowings,
			RegDBBorrowing{attr.PurePath(), use})
	}
}

// expandErrata expands db.Errata entries, not used before to
// replace the existent attributes (i.e., those Errata entries
// that injects new attributes).
func (db *RegDB) expandErrata() {
	// Collect all errata attributes in the sorted by name order
	names := make([]string, 0, len(db.Errata))
	for name := range db.Errata {
		names = append(names, name)
	}

	sort.Strings(names)

	// Now process one by one
	for _, name := range names {
		if attr := db.AllAttrs[name]; attr == nil {
			errata := db.Errata[name]
			err := db.add(errata)
			if err != nil {
				panic(err)
			}
		}
	}
}

// handleSuffixes handles attributes, marked by suffixes
// ("(extension)", "(deprecated)" etc) in their names.
func (db *RegDB) handleSuffixes() {
	// Roll over top-level collections
	names := db.CollectionNames()
	for _, name := range names {
		db.handleSuffixesInCollection(db.Collections[name])
	}

	// Roll over all attributes.
	//
	// Instead of recursive procession, we run over db.AllAttrs,
	// so dynamic changes in attribute membership doesn't affect
	// this work
	names = names[:0]
	for name := range db.AllAttrs {
		names = append(names, name)
	}

	sort.Strings(names)

	for _, name := range names {
		attr := db.AllAttrs[name]
		db.handleSuffixesInCollection(attr.Members)
	}

	// Rebuild db.AllAttrs
	db.rebuildAllAttrs()
}

// handleSuffixesInCollection does the real work of handling
// attribute suffixes.
func (db *RegDB) handleSuffixesInCollection(attrs map[string]*RegDBAttr) {
	// Gather aliases.
	//
	// Aliases are attributes with the same base name, but different
	// suffixes.
	allAliases := make(map[string][]*RegDBAttr)

	for _, attr := range attrs {
		name := attr.PureName()
		aliases := allAliases[name]
		aliases = append(aliases, attr)
		allAliases[name] = aliases
	}

	// Prepare sorted list of names, so things will be done
	// in the reproducible way.
	names := make([]string, 0, len(allAliases))
	for name := range allAliases {
		names = append(names, name)
	}

	sort.Strings(names)

	// Resolve aliases and rebuild attrs
	clear(attrs)
	for _, name := range names {
		aliases := allAliases[name]
		attr := aliases[0]
		if len(aliases) > 0 {
			var err error
			attr, err = db.resolveAliases(aliases)
			if err != nil {
				db.Errors = append(db.Errors, err)
			}
		}

		if attr != nil {
			attrs[name] = attr
		}
	}
}

// rebuildAllAttrs rebuilds db.AllAttrs, after all aliases are resolved.
func (db *RegDB) rebuildAllAttrs() {
	clear(db.AllAttrs)

	collections := db.CollectionNames()
	for _, col := range collections {
		db.rebuildAllAttrsRecursive(db.Collections[col])
	}
}

// rebuildAllAttrsRecursive does the real work of db.rebuildAllAttrs
// by visiting all attributes recursive.
func (db *RegDB) rebuildAllAttrsRecursive(attrs map[string]*RegDBAttr) {
	for _, attr := range attrs {
		db.AllAttrs[attr.PurePath()] = attr
		db.rebuildAllAttrsRecursive(attr.Members)
	}
}

// checkEmptyCollections identifies attributes that were marked
// as collections but have no members or inherited members.
// This is typically a sign of an incomplete registration.
func (db *RegDB) checkEmptyCollections() {
	collections := db.CollectionNames()
	for _, col := range collections {
		db.checkEmptyCollectionsRecursive(db.Collections[col])
	}
}

// checkEmptyCollectionsRecursive does the real work of checking
// for empty collections.
func (db *RegDB) checkEmptyCollectionsRecursive(attrs map[string]*RegDBAttr) {
	// Prepare sorted list of names, so things will be done
	// in the reproducible way.
	names := make([]string, 0, len(attrs))
	for name := range attrs {
		names = append(names, name)
	}

	sort.Strings(names)

	// Now roll over all names
	for _, name := range names {
		attr := attrs[name]
		if attr.Syntax.Collection && len(attr.Members) == 0 && len(attr.UseMembers) == 0 {
			err := fmt.Errorf("%s: collection missing member definitions", attr.Path())
			db.Errors = append(db.Errors, err)
		}

		db.checkEmptyCollectionsRecursive(attr.Members)
	}
}

func (db *RegDB) resolveAliases(aliases []*RegDBAttr) (*RegDBAttr, error) {
	var plain *RegDBAttr
	deprecated := []*RegDBAttr{}
	extension := []*RegDBAttr{}

	// Classify aliases
	for _, attr := range aliases {
		_, suffixes := attr.SplitName()
		switch {
		case suffixes == "":
			plain = attr
		case strings.Contains(suffixes, "extension"):
			extension = append(extension, attr)
		default:
			deprecated = append(deprecated, attr)
		}
	}

	// Now choose candidates
	candidates := extension
	if len(candidates) == 0 && plain != nil {
		candidates = append(candidates, plain)
	}
	if len(candidates) == 0 {
		candidates = append(candidates, deprecated...)
	}

	// Merge equal candidates
	end := 1
	for i := 1; i < len(candidates); i++ {
		attr := candidates[i]
		prev := candidates[i-1]

		if !attr.Syntax.Equal(prev.Syntax) {
			candidates[end] = attr
			end++
		}
	}

	candidates = candidates[:end]

	// If we only have a single candidate, everything is OK
	if len(candidates) == 1 {
		survivor := candidates[0]
		for _, attr := range aliases {
			if attr == survivor {
				continue
			}
			for name, mattr := range attr.Members {
				if _, exists := survivor.Members[name]; !exists {
					survivor.Members[name] = mattr
				}
			}
		}
		return survivor, nil
	}

	// Format the error message
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "Conflicting attribytes:\n")
	for _, attr := range candidates {
		fmt.Fprintf(buf, "  %s\n", attr.Path())
		fmt.Fprintf(buf, "  > %s\n", attr.SyntaxString)
	}

	return nil, errors.New(buf.String())
}

// newLink translates a registration record into a member-borrowing
// relationship. It handles both simple links (a points to b)
// and deeper links (a/b points to c).
func (db *RegDB) newLink(file string, line int, collection, name, member, submember string) (
	from, to string, err error) {

	var path []string

	switch {
	case name != "" && member != "" && submember == "":
		path = []string{collection, name}
		to = member

	case name != "" && member != "" && submember != "":
		path = []string{collection, name, member}
		to = submember

	default:
		panic("internal error")
	}

	from = strings.Join(path, "/")

	// The member names in the IANA XML sometimes have quotes or
	// "any attribute" markers. We strip those out to get the pure name.
	if fields := strings.Split(to, `"`); len(fields) == 3 {
		to = fields[1]
	} else if strings.HasPrefix(to, "<Any ") {
		to = strings.TrimPrefix(to, "<Any ")
		to = strings.TrimSuffix(to, " attribute>")
		to = strings.TrimSuffix(to, " Attribute>")
	}

	return
}

// newDirectLink creates new link directly, using absolute paths.
// It allows to create link targeted to the different top-level collection.
// Used only in the errata.xml with the following syntax:
func (db *RegDB) newDirectLink(file string, line int, from, to string) error {
	links := db.AddUseMembers[from]
	for _, link := range links {
		if link.Path == to {
			err := fmt.Errorf("%s:%d: %s->%s: duplicated link", file, line, from, to)
			return err
		}
	}

	db.AddUseMembers[from] = append(links, RegDBLink{Path: to, File: file, Line: line})

	return nil
}

// addSubst adds a substitution for the attribute link target
func (db *RegDB) addSubst(file string, line int, name, use string) error {
	if _, dup := db.Subst[name]; dup {
		err := fmt.Errorf("%s:%d: %s: duplicated substitution)", file, line, name)
		return err
	}

	db.Subst[name] = use
	return nil
}

// newRegDBAttr initializes a new attribute object. It performs
// name-splitting to identify parent collections and applies
// any overrides found in the errata database.
func (db *RegDB) newRegDBAttr(file string, line int, collection, name, member, submember,
	syntax, xref string) (*RegDBAttr, error) {

	// Create RegDBAttr structure
	attr := &RegDBAttr{
		Collection:   collection,
		SyntaxString: syntax,
		XRef:         xref,
		Members:      make(map[string]*RegDBAttr),
		File:         file,
		Line:         line,
	}

	// Populate Name and Parents
	switch {
	case name != "" && member == "" && submember == "":
		attr.Name = name

	case name != "" && member != "" && submember == "":
		attr.Name = member
		attr.Parents = []string{name}

	case name != "" && member != "" && submember != "":
		attr.Name = submember
		attr.Parents = []string{name, member}

	default:
		panic("internal error")
	}

	// Check for errata. If attribute is found in errata, it effectively
	// replaces normal attribute by the errata entry.
	if errata := db.Errata[attr.Path()]; errata != nil {
		return errata, nil
	}

	// Parse syntax
	var err error
	attr.Syntax, err = ParseSyntax(syntax)
	if err != nil {
		err = fmt.Errorf("%s: %s: %w", loc(file, line), attr.Path(), err)
	}

	return attr, err
}

// SplitName returns attribute's base name and suffixes, separated.
func (attr *RegDBAttr) SplitName() (name, suffixes string) {
	if i := strings.IndexByte(attr.Name, '('); i >= 0 {
		return attr.Name[:i], attr.Name[i:]
	}

	return attr.Name, ""
}

// PureName returns attribute name with possible suffixes stripped
func (attr *RegDBAttr) PureName() string {
	name, _ := attr.SplitName()
	return name
}

// Path returns full path to the attribute in the following form:
// "Collection/name/member/submember"
func (attr *RegDBAttr) Path() string {
	path := []string{attr.Collection}
	path = append(path, attr.Parents...)
	path = append(path, attr.Name)
	return strings.Join(path, "/")
}

// PurePath returns full path to the attribute with possible
// suffixes stripped in all path elements.
func (attr *RegDBAttr) PurePath() string {
	path := []string{attr.Collection}
	path = append(path, attr.Parents...)
	path = append(path, attr.Name)

	for pos, frag := range path {
		if i := strings.IndexByte(frag, '('); i >= 0 {
			path[pos] = frag[:i]
		}
	}

	return strings.Join(path, "/")
}
func loc(file string, line int) string {
	return fmt.Sprintf("%s:%d", strings.TrimPrefix(file, "/"), line)
}
