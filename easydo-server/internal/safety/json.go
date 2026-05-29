package safety

import (
	"fmt"
	"reflect"
	"strings"
)

type Options struct {
	MaxStringChars int
	MaxListItems   int
	MaxDepth       int
}

var sensitiveKeyParts = []string{
	"token",
	"secret",
	"password",
	"credential",
	"api_key",
	"authorization",
}

func DefaultOptions() Options {
	return Options{
		MaxStringChars: 4096,
		MaxListItems:   100,
		MaxDepth:       6,
	}
}

func SummarizeJSON(value any) any {
	return SanitizeJSONValue(value, DefaultOptions())
}

func SanitizeJSONValue(value any, opts Options) any {
	if opts.MaxStringChars < 1 {
		opts.MaxStringChars = DefaultOptions().MaxStringChars
	}
	if opts.MaxListItems < 1 {
		opts.MaxListItems = DefaultOptions().MaxListItems
	}
	if opts.MaxDepth < 1 {
		opts.MaxDepth = DefaultOptions().MaxDepth
	}
	return sanitize(value, opts, 1)
}

func sanitize(value any, opts Options, depth int) any {
	if depth > opts.MaxDepth {
		return "[truncated depth]"
	}

	switch v := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(v))
		for key, item := range v {
			if isSensitiveKey(key) {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = sanitize(item, opts, depth+1)
		}
		return result
	case []any:
		return sanitizeSlice(v, opts, depth)
	case string:
		return truncateString(v, opts.MaxStringChars)
	case fmt.Stringer:
		return truncateString(v.String(), opts.MaxStringChars)
	}

	rv := reflect.ValueOf(value)
	if !rv.IsValid() {
		return nil
	}

	switch rv.Kind() {
	case reflect.Map:
		if rv.Type().Key().Kind() != reflect.String {
			return truncateString(fmt.Sprintf("%v", value), opts.MaxStringChars)
		}
		result := make(map[string]any, rv.Len())
		iter := rv.MapRange()
		for iter.Next() {
			key := iter.Key().String()
			if isSensitiveKey(key) {
				result[key] = "[REDACTED]"
				continue
			}
			result[key] = sanitize(iter.Value().Interface(), opts, depth+1)
		}
		return result
	case reflect.Slice, reflect.Array:
		items := make([]any, rv.Len())
		for i := 0; i < rv.Len(); i++ {
			items[i] = rv.Index(i).Interface()
		}
		return sanitizeSlice(items, opts, depth)
	case reflect.Pointer, reflect.Interface:
		if rv.IsNil() {
			return nil
		}
		return sanitize(rv.Elem().Interface(), opts, depth)
	case reflect.Struct:
		return sanitizeStruct(rv, opts, depth)
	default:
		return value
	}
}

func sanitizeStruct(rv reflect.Value, opts Options, depth int) map[string]any {
	rt := rv.Type()
	result := make(map[string]any)
	for i := 0; i < rt.NumField(); i++ {
		field := rt.Field(i)
		if field.PkgPath != "" {
			continue
		}
		name, ok := jsonFieldName(field)
		if !ok {
			continue
		}
		if isSensitiveKey(field.Name) || isSensitiveKey(name) {
			result[name] = "[REDACTED]"
			continue
		}
		result[name] = sanitize(rv.Field(i).Interface(), opts, depth+1)
	}
	return result
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "-" {
		return "", false
	}
	if tag != "" {
		name := strings.Split(tag, ",")[0]
		if name != "" {
			return name, true
		}
	}
	return field.Name, true
}

func sanitizeSlice(items []any, opts Options, depth int) []any {
	limit := len(items)
	truncated := false
	if limit > opts.MaxListItems {
		limit = opts.MaxListItems
		truncated = true
	}
	result := make([]any, 0, limit)
	for i := 0; i < limit; i++ {
		result = append(result, sanitize(items[i], opts, depth+1))
	}
	if truncated && len(result) > 0 {
		result[len(result)-1] = fmt.Sprintf("[truncated %d items]", len(items)-opts.MaxListItems+1)
	}
	return result
}

func truncateString(value string, max int) string {
	runes := []rune(value)
	if len(runes) <= max {
		return value
	}
	marker := "...[truncated]"
	markerRunes := []rune(marker)
	if max <= len(markerRunes) {
		return string(markerRunes[:max])
	}
	return string(runes[:max-len(markerRunes)]) + marker
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	for _, part := range sensitiveKeyParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}
