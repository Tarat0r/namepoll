package form

import (
	"fmt"
	"os"
	"strings"

	"go.yaml.in/yaml/v3"
)

func Loader(path string) (*Definition, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read form file: %w", err)
	}
	return Parse(string(data))
}

// Parse loads and validates a form definition from YAML text.
func Parse(data string) (*Definition, error) {
	var definition Definition
	decoder := yaml.NewDecoder(strings.NewReader(data))
	decoder.KnownFields(true)
	if err := decoder.Decode(&definition); err != nil {
		return nil, fmt.Errorf("parse form configuration: %w", err)
	}

	if err := definition.Validate(); err != nil {
		return nil, fmt.Errorf("invalid form: %w", err)
	}

	return &definition, nil
}
