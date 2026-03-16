package main

import (
	"strings"
	"testing"

	"github.com/OpenPrinting/go-mfp/util/xmldoc"
)

func TestValidateValues(t *testing.T) {
	tests := []struct {
		name       string
		xmlContent string
		setupDB    func(*RegDB)
		wantErrors []string
	}{
		{
			name: "Valid Enum",
			xmlContent: `
			<root>
				<registry id="ipp-registrations-6">
					<record>
						<attribute>system-state</attribute>
						<value>3</value>
					</record>
				</registry>
			</root>
			`,
			setupDB: func(db *RegDB) {
				db.AllAttrs["System Status/system-state"] = &RegDBAttr{
					Name: "system-state",
					Collection: "System Status",
					SyntaxString: "enum",
				}
			},
			wantErrors: nil,
		},
		{
			name: "Invalid Enum Format",
			xmlContent: `
			<root>
				<registry id="ipp-registrations-6">
					<record>
						<attribute>system-state</attribute>
						<value>not-a-number</value>
					</record>
				</registry>
			</root>
			`,
			setupDB: func(db *RegDB) {
				db.AllAttrs["System Status/system-state"] = &RegDBAttr{
					Name: "system-state",
					Collection: "System Status",
					SyntaxString: "enum",
				}
			},
			wantErrors: []string{"system-state/not-a-number: enum value is not a valid number"},
		},
		{
			name: "Valid Keyword",
			xmlContent: `
			<root>
				<registry id="ipp-registrations-4">
					<record>
						<attribute>media-col</attribute>
						<value>auto-select</value>
					</record>
				</registry>
			</root>
			`,
			setupDB: func(db *RegDB) {
				db.AllAttrs["Job Template/media-col"] = &RegDBAttr{
					Name: "media-col",
					Collection: "Job Template",
					SyntaxString: "keyword",
				}
			},
			wantErrors: nil,
		},
		{
			name: "Invalid Keyword Format",
			xmlContent: `
			<root>
				<registry id="ipp-registrations-4">
					<record>
						<attribute>media-col</attribute>
						<value>Invalid_Keyword!</value>
					</record>
				</registry>
			</root>
			`,
			setupDB: func(db *RegDB) {
				db.AllAttrs["Job Template/media-col"] = &RegDBAttr{
					Name: "media-col",
					Collection: "Job Template",
					SyntaxString: "keyword",
				}
			},
			wantErrors: []string{"media-col/Invalid_Keyword!: keyword value is not a valid keyword"},
		},
		{
			name: "Missing Values Table for Enum",
			xmlContent: `
			<root>
				<registry id="ipp-registrations-6">
				</registry>
			</root>
			`,
			setupDB: func(db *RegDB) {
				attr := &RegDBAttr{
					Name: "missing-enum-attr",
					Collection: "Job Status",
					Parents: []string{},
					File: "test.xml",
					Line: 42,
					SyntaxString: "enum",
				}
				db.AllAttrs["Job Status/missing-enum-attr"] = attr
			},
			wantErrors: []string{"test.xml:42: Job Status/missing-enum-attr: enum/keyword attribute has no registered values table"},
		},
		{
			name: "Values Table References Unknown Attribute",
			xmlContent: `
			<root>
				<registry id="ipp-registrations-4">
					<record>
						<attribute>phantom-attr</attribute>
						<value>test-val</value>
					</record>
				</registry>
			</root>
			`,
			setupDB: func(db *RegDB) {
				// empty DB
			},
			wantErrors: []string{"phantom-attr: values table references unknown attribute"},
		},
		{
			name: "Ignore <Any > Patterns",
			xmlContent: `
			<root>
				<registry id="ipp-registrations-4">
					<record>
						<attribute>&lt;Any "media" value&gt;</attribute>
						<value>&lt;Any value&gt;</value>
					</record>
				</registry>
			</root>
			`,
			setupDB: func(db *RegDB) {
				// Should ignore, no errors because those prefixes skip checks
			},
			wantErrors: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			
			// Parse the inline XML
			xmlNode, err := xmldoc.Decode(nil, strings.NewReader(tt.xmlContent))
			if err != nil {
				t.Fatalf("Failed to parse mock XML: %v", err)
			}
			
			// Custom strip logic for unit tests: we must simulate xmlCleanup here
			// where names get the '-:' removed and whitespace trimmed
			var cleanup func(*xmldoc.Element)
			cleanup = func(root *xmldoc.Element) {
				root.Name, _ = strings.CutPrefix(root.Name, "-:")
				root.Text = strings.TrimSpace(root.Text)
				for i := range root.Children {
					chld := &root.Children[i]
					cleanup(chld)
				}
			}
			cleanup(&xmlNode)
			
			db := NewRegDB()
			if tt.setupDB != nil {
				tt.setupDB(db)
			}

			db.ValidateValues("test.xml", xmlNode)

			if len(db.Errors) != len(tt.wantErrors) {
				t.Errorf("got %d errors, want %d: %v", len(db.Errors), len(tt.wantErrors), db.Errors)
			}

			for i, we := range tt.wantErrors {
				if i < len(db.Errors) && !strings.Contains(db.Errors[i].Error(), we) {
					t.Errorf("error %d: got %q, want it to contain %q", i, db.Errors[i].Error(), we)
				}
			}
		})
	}
}
