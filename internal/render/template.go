package render

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
)

//go:embed report.schema.json
var templateSchemaJSON []byte

var templateSchema, templateSchemaErr = compileTemplateSchema()

func compileTemplateSchema() (*jsonschema.Schema, error) {
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(templateSchemaJSON))
	if err != nil {
		return nil, err
	}
	compiler := jsonschema.NewCompiler()
	const location = "urn:qfreportpdf:report-schema"
	if err := compiler.AddResource(location, document); err != nil {
		return nil, err
	}
	return compiler.Compile(location)
}

// ParseTemplate validates the layout and the references between its parts.
func ParseTemplate(raw []byte) (Report, error) {
	if templateSchemaErr != nil {
		return Report{}, fmt.Errorf("compile embedded report schema: %w", templateSchemaErr)
	}
	document, err := jsonschema.UnmarshalJSON(bytes.NewReader(raw))
	if err != nil {
		return Report{}, fmt.Errorf("template: decode JSON: %w", err)
	}
	if err := templateSchema.Validate(document); err != nil {
		return Report{}, fmt.Errorf("template: %w", err)
	}
	var report Report
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&report); err != nil {
		return Report{}, fmt.Errorf("template: decode layout: %w", err)
	}
	if err := validateTemplate(report); err != nil {
		return Report{}, err
	}
	return report, nil
}

type declaredColumns map[string]map[string]bool

func validateTemplate(report Report) error {
	for name, font := range report.Fonts {
		if !strings.HasPrefix(font.URL, "data:") {
			return fmt.Errorf("template.fonts[%q].url: external font requires a file or network request. Embed a TrueType font as a base64 data URI", name)
		}
	}
	columns := make(declaredColumns, len(report.DataSources))
	for name, source := range report.DataSources {
		fields := make(map[string]bool, len(source.Columns))
		for i, column := range source.Columns {
			if fields[column.Name] {
				return fmt.Errorf("template.dataSources[%q].columns[%d].name: duplicate column %q", name, i, column.Name)
			}
			fields[column.Name] = true
		}
		columns[name] = fields
	}
	var parent *Band // The band that a child band follows.
	for i, band := range report.Bands {
		path := fmt.Sprintf("template.bands[%d]", i)
		if band.DataSource != nil && columns[*band.DataSource] == nil {
			return fmt.Errorf("%s.dataSource: unknown source %q", path, *band.DataSource)
		}
		switch {
		case band.Kind != BandChild:
			parent = &report.Bands[i]
		case parent == nil:
			return fmt.Errorf("%s.kind: a child band must follow another band", path)
		case band.DataSource != nil && *band.DataSource != bandDataSource(*parent):
			return fmt.Errorf("%s.dataSource: source %q differs from band %q, which the child band follows", path, *band.DataSource, parent.ID)
		}
		if band.GroupExpr != nil {
			if err := columns.checkText(path+".groupExpr", *band.GroupExpr); err != nil {
				return err
			}
		}
		if len(band.GroupBy) > 0 {
			source := bandDataSource(band)
			if source == "" {
				return fmt.Errorf("%s.groupBy: grouping columns require a data source", path)
			}
			for j, field := range band.GroupBy {
				if !columns[source][field] {
					return fmt.Errorf("%s.groupBy[%d]: unknown column %q in source %q", path, j, field, source)
				}
			}
		}
		for j, element := range band.Elements {
			if err := columns.checkElement(fmt.Sprintf("%s.elements[%d]", path, j), element); err != nil {
				return err
			}
		}
	}
	return nil
}

func (columns declaredColumns) checkElement(path string, element Element) error {
	switch element.Type {
	case ElementText, ElementBarcode:
		if element.Text == "" && element.Binding == "" && element.Expression == "" {
			return fmt.Errorf("%s: %s requires non-empty text, binding, or expression", path, element.Type)
		}
		if element.URL != "" {
			link, err := url.Parse(element.URL)
			if err != nil || (link.Scheme != "" && link.Scheme != "http" && link.Scheme != "https" && link.Scheme != "file") {
				return fmt.Errorf("%s.url: invalid hyperlink", path)
			}
		}
	case ElementImage:
		if element.Image == nil && !strings.HasPrefix(element.URL, "data:") {
			return fmt.Errorf("%s.url: external image requires a file or network request. Embed base64 image data", path)
		}
	}
	if element.Binding != "" && element.Binding != "[PAGE()]" && element.Binding != "[PAGETOTAL()]" {
		source, field, ok := parseBinding(element.Binding)
		if !ok {
			return fmt.Errorf("%s.binding: expected a source.column binding, got %q", path, element.Binding)
		}
		if err := columns.checkBinding(path+".binding", element.Binding, source, field); err != nil {
			return err
		}
	}
	if err := columns.checkText(path+".text", element.Text); err != nil {
		return err
	}
	return columns.checkText(path+".expression", element.Expression)
}

func (columns declaredColumns) checkText(path, text string) error {
	for _, match := range bindingPattern.FindAllStringSubmatch(text, -1) {
		if err := columns.checkBinding(path, match[0], match[1], match[2]); err != nil {
			return err
		}
	}
	return nil
}

func (columns declaredColumns) checkBinding(path, binding, source, field string) error {
	if columns[source] == nil {
		return fmt.Errorf("%s: binding %q references unknown source %q", path, binding, source)
	}
	if !columns[source][field] {
		return fmt.Errorf("%s: binding %q references unknown column %q in source %q", path, binding, field, source)
	}
	return nil
}
