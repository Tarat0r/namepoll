package form

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
)

var fieldNamePattern = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)
var colorPattern = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

func (d Definition) Validate() error {
	if strings.TrimSpace(d.Title) == "" {
		return fmt.Errorf("title is required")
	}

	if strings.TrimSpace(d.Description) == "" {
		return fmt.Errorf("description is required")
	}

	if d.StartDate.IsZero() {
		return fmt.Errorf("start_date is required")
	}

	if d.EndDate.IsZero() {
		return fmt.Errorf("end_date is required")
	}

	if !d.EndDate.After(d.StartDate) {
		return fmt.Errorf("end_date must be after start_date")
	}
	if d.Statistics.AccessToken != strings.TrimSpace(d.Statistics.AccessToken) {
		return fmt.Errorf("statistics.access_token cannot start or end with whitespace")
	}
	if err := d.Theme.Validate(); err != nil {
		return err
	}
	seenFields := make(map[string]struct{}, len(d.Fields))
	for i, field := range d.Fields {
		if err := field.Validate(); err != nil {
			return fmt.Errorf("field %d: %w", i, err)
		}
		if _, exists := seenFields[field.Name]; exists {
			return fmt.Errorf("field %d: duplicate name %q", i, field.Name)
		}
		seenFields[field.Name] = struct{}{}
	}

	return nil
}

func (t ThemeConfig) Validate() error {
	colors := []struct {
		name  string
		value string
	}{
		{name: "background", value: t.Background},
		{name: "surface", value: t.Surface},
		{name: "text", value: t.Text},
		{name: "accent", value: t.Accent},
		{name: "accent_strong", value: t.AccentStrong},
		{name: "field", value: t.Field},
		{name: "field_alternate", value: t.FieldAlternate},
	}

	for _, color := range colors {
		if color.value != "" && !colorPattern.MatchString(color.value) {
			return fmt.Errorf("theme.%s must be a six-digit hex color", color.name)
		}
	}

	return nil
}

func (f Field) Validate() error {
	if strings.TrimSpace(f.Name) == "" {
		return fmt.Errorf("name is required")
	}
	if !fieldNamePattern.MatchString(f.Name) || f.Name == "author_name" {
		return fmt.Errorf("invalid name %q", f.Name)
	}
	if strings.TrimSpace(f.Label) == "" {
		return fmt.Errorf("label is required")
	}
	if f.Type != "text" {
		return fmt.Errorf("unsupported type: %q", f.Type)
	}
	if f.Count == 0 {
		return fmt.Errorf("count must be at least 1")
	}
	if f.RequiredCount == 0 {
		return fmt.Errorf("required_count must be at least 1")
	}
	if f.RequiredCount > f.Count {
		return fmt.Errorf("required_count cannot be greater than count")
	}
	return nil
}

func NormalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", fmt.Errorf("name can not be empty")
	}

	runes := []rune(name)

	for _, r := range runes {
		if !unicode.In(r, unicode.Cyrillic) && r != '-' {
			return "", fmt.Errorf("name can contain only cyrillic letters and '-'")
		}
	}

	if runes[0] == '-' || runes[len(runes)-1] == '-' {
		return "", fmt.Errorf("name cannot start or end with '-'")
	}

	if strings.Contains(name, "--") {
		return "", fmt.Errorf("name cannot contain consecutive '-'")
	}

	for i := range runes {
		if i == 0 || runes[i-1] == '-' {
			runes[i] = unicode.ToUpper(runes[i])
		} else {
			runes[i] = unicode.ToLower(runes[i])
		}
	}

	return string(runes), nil
}
