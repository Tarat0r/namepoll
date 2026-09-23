package form

import "time"

type Definition struct {
	Title       string           `yaml:"title"`
	Description string           `yaml:"description"`
	StartDate   time.Time        `yaml:"start_date"`
	EndDate     time.Time        `yaml:"end_date"`
	Theme       ThemeConfig      `yaml:"theme"`
	Statistics  StatisticsConfig `yaml:"statistics"`
	Fields      []Field          `yaml:"fields"`
}

type ThemeConfig struct {
	Background     string `yaml:"background"`
	Surface        string `yaml:"surface"`
	Text           string `yaml:"text"`
	Accent         string `yaml:"accent"`
	AccentStrong   string `yaml:"accent_strong"`
	Field          string `yaml:"field"`
	FieldAlternate string `yaml:"field_alternate"`
}

type StatisticsConfig struct {
	PublicAfterEnd bool   `yaml:"public_after_end"`
	AccessToken    string `yaml:"access_token"`
}

type Field struct {
	Name          string `yaml:"name"`
	Label         string `yaml:"label"`
	Type          string `yaml:"type"`
	Count         uint   `yaml:"count"`
	RequiredCount uint   `yaml:"required_count"`
}
