package mcp

import "easydo-server/internal/safety"

func SanitizeSummary(value any) any {
	return safety.SanitizeJSONValue(value, safety.Options{
		MaxStringChars: MaxOutputStringChars,
		MaxListItems:   MaxOutputListItems,
		MaxDepth:       MaxJSONDepth,
	})
}
