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

// RegDB represents database of IANA registrations for IPP
type RegDB struct {
	Collections   map[string]map[string]*RegDBAttr // Attrs by collection
	AllAttrs      map[string]*RegDBAttr            // All attrs, by path
	AddUseMembers map[string][]string              // Added attr.UseMembers
	Subst         map[string]string                // Abs paths for links
	ErrataSkip    generic.Set[string]              // Errata: ignored attrs
	Errata        map[string]*RegDBAttr            // Errata: replaced by path
	Errors        []error                          // Collected errors
	Borrowings    []RegDBBorrowing                 // Members borrowings
	Exceptions    generic.Set[string]              // Excluded members
}

// RegDBBorrowing represents relations between collection attributes,
// where the RegDBBorrowing.From attribute borrows members from the
// RegDBBorrowing.To attribute.
type RegDBBorrowing struct {
	From string
	To   string
}

// RegDBAttr represents a single attribute
type RegDBAttr struct {
	Name         string                // Attribute name
	Collection   string                // Collection it belongs to
	Parents      []string              // Parent collections
	SyntaxString string                // Syntax string
	Syntax       Syntax                // Attribute syntax, parsed
	XRef         string                // Document it is defined in
	UseMembers   []string              // Use members from other attr
	Members      map[string]*RegDBAttr // Members, by name
	File         string                // File where it was defined
	Line         int                   // Line number in the file
}

// NewRegDB creates a new RegDB
func NewRegDB() *RegDB {
	return &RegDB{
		Collections:   make(map[string]map[string]*RegDBAttr),
		AllAttrs:      make(map[string]*RegDBAttr),
		AddUseMembers: make(map[string][]string),
		Subst:         make(map[string]string),
		ErrataSkip:    generic.NewSet[string](),
		Errata:        make(map[string]*RegDBAttr),
		Exceptions:    generic.NewSet[string](),
	}
}

// Load loads attributes from XML
func (db *RegDB) Load(file string, xml xmldoc.Element, errata bool) error {
	for _, registry := range xml.Children {
		// Ignore elements other that "registry"
		if registry.Name != "registry" {
			continue
		}

		// Process "record" elements
		for _, record := range registry.Children {
			// Ignore elements other that "record"
			if record.Name != "record" {
				continue
			}

			err := db.loadRecord(file, record, errata)
			if err != nil {
				return err
			}
		}

		// Process "use-members", "skip" and "subst" elements;
		// only in errata
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

// Finalize must be called after all attribute files are loaded.
// It acts as a second pass over the database to build cross-references
// (like resolving aliases and links) and validate structural integrity
// (like catching collections that are completely empty).
func (db *RegDB) Finalize() error {
	db.expandErrata()
	db.handleSuffixes()
	db.resolveLinks()
	db.checkEmptyCollections()
	return nil
}

// loadRecord handles the "record" element, which contains
// attribute description.
func (db *RegDB) loadRecord(file string, record xmldoc.Element, errata bool) error {
	// Lookup fields we are interested in
	collection := xmldoc.Lookup{Name: "collection", Required: true}
	name := xmldoc.Lookup{Name: "name", Required: true}
	syntax := xmldoc.Lookup{Name: "syntax", Required: true}
	xref := xmldoc.Lookup{Name: "xref", Required: true}
	member := xmldoc.Lookup{Name: "member_attribute"}
	submember := xmldoc.Lookup{Name: "sub-member_attribute"}

	missed := record.Lookup(&collection, &name, &syntax, &xref,
		&member, &submember)

	if missed != nil {
		err := fmt.Errorf("%s:%d: record missing required field: %q", file, record.Line, missed.Name)
		db.Errors = append(db.Errors, err)
		return nil
	}

	// Flag empty or self-closing syntax tags. The IANA database uses
	// <syntax/> for link records, but we still want to track them.
	if syntax.Elem.Text == "" {
		attrName := name.Elem.Text
		if member.Elem.Text != "" {
			attrName = name.Elem.Text + "/" + member.Elem.Text
		}
		if submember.Elem.Text != "" {
			attrName = attrName + "/" + submember.Elem.Text
		}
		err := fmt.Errorf("%s:%d: %s/%s: empty syntax field",
			file, syntax.Elem.Line, collection.Elem.Text, attrName)
		db.Errors = append(db.Errors, err)

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
				attr.UseMembers = append(attr.UseMembers, to)
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

// loadUseMembers handles the "use-members" element, inserts attribute
// borrowing (so recipient attribute will use members, defined
// for some other attribute).
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
				err := fmt.Errorf("%s:%d: %q: duplicated exception", file, link.Line, path)
				return err
			}
		}
	}

	return nil
}

// loadSkip handles the "skip" element, which causes named
// attribute to be ignored.
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

// loadSubst handles the "subst" elements, that defines the full path
// to the link target (for example, media-col->Job Template/media-col).
//
// This is useful, when link target is ambiguous.
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
		err := fmt.Errorf("%s:%d: %s: duplicated errata attribute", attr.File, attr.Line, path)
		return err
	}

	db.Errata[path] = attr
	return nil
}

// add adds attribute to the database
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
			err := fmt.Errorf("%s:%d: %s: no parent (%s)",
				attr.File, attr.Line, attr.Path(), strings.Join(path, "/"))
			return err
		}
	}

	// Check for duplicates and save attribute
	pmap := collection
	if parent != nil {
		pmap = parent.Members
	}

	if attr2 := pmap[attr.Name]; attr2 != nil {
		err := fmt.Errorf("%s:%d: %s: conflicts with %s",
			attr.File, attr.Line, attr.Path(), attr2.Path())
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

// resolveLinks resolves links between attributes
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

// resolveLink resolves link of the single attribute:
//
//	attr.UseMembers becomes absolute
//	attr.Borrowed populated
func (db *RegDB) resolveLink(attr *RegDBAttr) {
	// Lookup global db.AddUseMembers
	if added := db.AddUseMembers[attr.Path()]; added != nil {
		attr.UseMembers = append(attr.UseMembers, added...)
	}

	// Roll over all attr.UseMembers
	for i, use := range attr.UseMembers {
		// Lookup substitutions
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
					attr.File, attr.Line, attr.Path(), use)
				db.Errors = append(db.Errors, err)

			case attr2 == attr:
				err = fmt.Errorf("%s:%d: %s->%s: link to self",
					attr.File, attr.Line, attr.Path(), use)
				db.Errors = append(db.Errors, err)

			case len(attr2.Members) == 0:
				err = fmt.Errorf("%s:%d: %s->%s: link target enpty",
					attr.File, attr.Line, attr.Path(), use)
				db.Errors = append(db.Errors, err)
			}

			if err != nil {
				continue
			}
		}

		// Save resolved link
		attr.UseMembers[i] = use
		db.Borrowings = append(db.Borrowings,
			RegDBBorrowing{attr.PurePath(), use})
	}
}

// expandErrata injects any errata attributes that weren't already used to
// overwrite existing attributes. This handles the case where errata adds
// entirely new attributes to the database rather than just fixing old ones.
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

// handleSuffixes groups attributes that share a base name but have
// different suffixes like "(extension)" or "(deprecated)". This helps
// us choose the most relevant definition (e.g. prioritize extensions)
// and flags conflicts if definitions contradict each other.
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

// checkEmptyCollections checks for collection attributes
// without members.
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
			err := fmt.Errorf("%s:%d: %s: collection missing member definitions", attr.File, attr.Line, attr.Path())
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
		return candidates[0], nil
	}

	// Format the error message
	buf := &bytes.Buffer{}
	fmt.Fprintf(buf, "Conflicting attribytes:\n")
	for _, attr := range candidates {
		fmt.Fprintf(buf, "  %s:%d: %s\n", attr.File, attr.Line, attr.Path())
		fmt.Fprintf(buf, "  > %s\n", attr.SyntaxString)
	}

	return nil, errors.New(buf.String())
}

// newLink prepares new link from one collection attribute to another.
// Link means that one attribute borrows members from another.
//
// In the XML it looks as follows:
//
//	<record>
//	  <collection>Document Status</collection>
//	  <name>cover-back-actual</name>
//	  <member_attribute>&lt;Any "cover-back" member attribute&gt;</member_attribute>
//	  <syntax/>
//	  <xref type="uri" data="https://ftp.pwg.org/pub/pwg/candidates/cs-ippdocobject12-20240517-5100.5.pdf">PWG5100.5</xref>
//	</record>
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

	if fields := strings.Split(to, `"`); len(fields) == 3 {
		err := fmt.Errorf("%s:%d: %s: broken collection reference %q", file, line, from, to)
		db.Errors = append(db.Errors, err)
		to = fields[1]
	} else if strings.HasPrefix(to, "<Any ") {
		err := fmt.Errorf("%s:%d: %s: broken collection reference %q", file, line, from, to)
		db.Errors = append(db.Errors, err)
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
		if link == to {
			err := fmt.Errorf("%s:%d: %s->%s: duplicated link", file, line, from, to)
			return err
		}
	}

	db.AddUseMembers[from] = append(links, to)

	return nil
}

// addSubst adds a substitution for the attribute link target
func (db *RegDB) addSubst(file string, line int, name, use string) error {
	if _, dup := db.Subst[name]; dup {
		err := fmt.Errorf("%s:%d: %s: duplicated substutution)", file, line, name)
		return err
	}

	db.Subst[name] = use
	return nil
}

// newRegDBAttr creates a new RegDBAttr
func (db *RegDB) newRegDBAttr(file string, line int, collection, name, member, submember,
	syntax, xref string) (*RegDBAttr, error) {

	// Create RegDBAttr structure
	attr := &RegDBAttr{
		File:         file,
		Line:         line,
		Collection:   collection,
		SyntaxString: syntax,
		XRef:         xref,
		Members:      make(map[string]*RegDBAttr),
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
	//
	// Do it before we attempt to parse the syntax, so broken syntax
	// can be fixed this way.
	if errata := db.Errata[attr.Path()]; errata != nil {
		return errata, nil
	}

	// Parse syntax
	var err error
	attr.Syntax, err = ParseSyntax(syntax)
	if err != nil {
		err = fmt.Errorf("%s: %w", attr.Path(), err)
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
