package render

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var uuidV4Pattern = regexp.MustCompile(`(?i)^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)

// ParseData validates the rows for each data source and converts each cell to display text.
func ParseData(raw []byte, sources map[string]DataSource) (map[string][]map[string]string, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var input map[string]any
	if err := decoder.Decode(&input); err != nil {
		return nil, fmt.Errorf("data: expected a JSON object: %w", err)
	}
	if input == nil {
		return nil, fmt.Errorf("data: expected a JSON object")
	}
	if len(bytes.Trim(raw[decoder.InputOffset():], " \t\r\n")) != 0 {
		return nil, fmt.Errorf("data: unexpected content after JSON object")
	}
	for name := range input {
		if _, exists := sources[name]; !exists {
			return nil, fmt.Errorf("data.%s: unknown data source", name)
		}
	}
	// result["orders"][i]["quantity"] is the display text for quantity in the i-th order.
	result := make(map[string][]map[string]string, len(sources))
	for name, source := range sources {
		value, exists := input[name]
		if !exists {
			return nil, fmt.Errorf("data.%s: missing data source", name)
		}
		rows, ok := value.([]any)
		if !ok {
			return nil, fmt.Errorf("data.%s: expected an array of rows", name)
		}
		columns := make(map[string]ColumnType, len(source.Columns))
		for _, column := range source.Columns {
			columns[column.Name] = column.Type
		}
		formattedRows := make([]map[string]string, len(rows))
		for i, row := range rows {
			cells, ok := row.(map[string]any)
			if !ok {
				return nil, fmt.Errorf("data.%s[%d]: expected a row object", name, i)
			}
			formattedRows[i] = make(map[string]string, len(cells))
			for field, value := range cells {
				kind, exists := columns[field]
				if !exists {
					return nil, fmt.Errorf("data.%s[%d].%s: unknown column", name, i, field)
				}
				cell, err := formatCell(kind, value)
				if err != nil {
					return nil, fmt.Errorf("data.%s[%d].%s: %w", name, i, field, err)
				}
				formattedRows[i][field] = cell
			}
		}
		result[name] = formattedRows
	}
	return result, nil
}

func formatCell(kind ColumnType, value any) (string, error) {
	if value == nil {
		return "", nil
	}
	switch kind {
	case ColumnString, ColumnExpression:
		if s, ok := value.(string); ok {
			return s, nil
		}
	case ColumnDate:
		if s, ok := value.(string); ok {
			if _, err := time.Parse(time.DateOnly, s); err == nil {
				return s, nil
			}
		}
		return "", fmt.Errorf("expected a date string in YYYY-MM-DD format")
	case ColumnUUIDv4:
		if s, ok := value.(string); ok && uuidV4Pattern.MatchString(s) {
			return s, nil
		}
	case ColumnBoolean:
		if b, ok := value.(bool); ok {
			return strconv.FormatBool(b), nil
		}
	case ColumnDecimal, ColumnInteger:
		if n, ok := value.(json.Number); ok && (kind == ColumnDecimal || isIntegral(n.String())) {
			return n.String(), nil
		}
	}
	return "", fmt.Errorf("expected %s", kind)
}

// isIntegral checks a valid JSON number without rounding or expanding its exponent.
func isIntegral(number string) bool {
	mantissa := strings.TrimPrefix(number, "-")
	var exponent int64
	if i := strings.IndexAny(mantissa, "eE"); i >= 0 {
		// ParseInt saturates on overflow, beyond any digit count in the input.
		exponent, _ = strconv.ParseInt(mantissa[i+1:], 10, 64)
		mantissa = mantissa[:i]
	}
	whole, fraction, _ := strings.Cut(mantissa, ".")
	if significant := strings.TrimRight(fraction, "0"); significant != "" {
		return exponent >= int64(len(significant))
	}
	if whole == "0" {
		return true
	}
	trailingZeros := len(whole) - len(strings.TrimRight(whole, "0"))
	return exponent >= -int64(trailingZeros)
}
