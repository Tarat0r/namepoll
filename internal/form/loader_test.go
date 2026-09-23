package form

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestLoaderRejectsUnknownConfigurationKeys(t *testing.T) {
	path := filepath.Join(t.TempDir(), "form.yaml")
	contents := `
title: Test
description: Test form
start_date: 2026-01-01T10:00:00Z
end_date: 2026-01-02T10:00:00Z
unknown_setting: true
statistics:
  public_after_end: true
fields:
  - name: suggestions
    label: Suggestions
    type: text
    count: 2
    required_count: 1
`
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatalf("write form config: %v", err)
	}

	_, err := Loader(path)
	if err == nil || !strings.Contains(err.Error(), "unknown_setting") {
		t.Fatalf("Loader() error = %v, want unknown field error", err)
	}
}

func TestDefaultConfigurationLoads(t *testing.T) {
	definition, err := Loader(filepath.Join("..", "..", "forms", "default.yaml"))
	if err != nil {
		t.Fatalf("load default configuration: %v", err)
	}
	if len(definition.Fields) == 0 {
		t.Fatal("default configuration has no fields")
	}
}

func TestInlineConfigurationLoads(t *testing.T) {
	definition, err := Parse(`
title: "Кое име ти харесва?"
description: "Сподели любимите си предложения."
start_date: 2026-09-20T10:00:00+03:00
end_date: 2026-09-25T23:59:59+03:00
theme:
  background: "#fffdfd"
  surface: "#fff7fa"
  text: "#4b2b37"
  accent: "#d7829d"
  accent_strong: "#a84b6a"
  field: "#fffdfd"
  field_alternate: "#f8edf1"
statistics:
  public_after_end: false
  access_token: ""
fields:
  - name: suggestions
    label: Предложения
    type: text
    count: 5
    required_count: 1
`)
	if err != nil {
		t.Fatalf("Parse() error = %v", err)
	}
	if definition.Theme.Accent == "" {
		t.Fatal("inline configuration has no theme accent")
	}
}

func TestDefinitionValidationRejectsInvalidFields(t *testing.T) {
	base := Definition{
		Title:       "Test",
		Description: "Test form",
		StartDate:   time.Date(2026, 1, 1, 10, 0, 0, 0, time.UTC),
		EndDate:     time.Date(2026, 1, 2, 10, 0, 0, 0, time.UTC),
		Fields: []Field{
			{Name: "suggestions", Label: "Suggestions", Type: "text", Count: 2, RequiredCount: 1},
		},
	}

	tests := []struct {
		name   string
		mutate func(*Definition)
	}{
		{
			name: "required count above input count",
			mutate: func(definition *Definition) {
				definition.Fields[0].RequiredCount = 3
			},
		},
		{
			name: "duplicate field name",
			mutate: func(definition *Definition) {
				definition.Fields = append(definition.Fields, definition.Fields[0])
			},
		},
		{
			name: "reserved field name",
			mutate: func(definition *Definition) {
				definition.Fields[0].Name = "author_name"
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			definition := base
			definition.Fields = append([]Field(nil), base.Fields...)
			test.mutate(&definition)
			if err := definition.Validate(); err == nil {
				t.Fatal("Validate() succeeded for invalid definition")
			}
		})
	}
}

func TestThemeValidation(t *testing.T) {
	for _, test := range []struct {
		name    string
		color   string
		wantErr bool
	}{
		{name: "empty uses defaults", color: ""},
		{name: "six digit hex", color: "#Aa00fF"},
		{name: "named color", color: "pink", wantErr: true},
		{name: "short hex", color: "#fff", wantErr: true},
		{name: "css injection", color: "#ffffff; color: red", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			err := (ThemeConfig{Accent: test.color}).Validate()
			if (err != nil) != test.wantErr {
				t.Fatalf("Validate() error = %v, wantErr %v", err, test.wantErr)
			}
		})
	}
}
