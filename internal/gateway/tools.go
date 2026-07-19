package gateway

import (
	"strings"
	"unicode"
)

// SanitizeFunctionName normalizes a tool name for upstream APIs.
func SanitizeFunctionName(raw string) string {
	var b strings.Builder
	for _, c := range raw {
		if (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9') || c == '_' || c == '-' {
			b.WriteRune(c)
		} else {
			b.WriteRune('_')
		}
	}
	name := strings.ReplaceAll(b.String(), "__", "_")
	for strings.Contains(name, "__") {
		name = strings.ReplaceAll(name, "__", "_")
	}
	name = strings.Trim(name, "_")

	if name == "" || !isValidNamePrefix(name) {
		name = "fn_" + name
	}
	if len(name) > maxFunctionNameLen {
		name = strings.TrimRight(name[:maxFunctionNameLen], "_")
	}
	for len(name) < minFunctionNameLen {
		name += "_"
	}
	if name == "" {
		return "fn_tool"
	}
	return name
}

func isValidNamePrefix(name string) bool {
	if name == "" {
		return false
	}
	r := rune(name[0])
	return unicode.IsLetter(r) || r == '_'
}

func uniqueFunctionName(raw string, registry map[string]string) string {
	if existing, ok := registry[raw]; ok {
		return existing
	}
	base := SanitizeFunctionName(raw)
	candidate := base
	suffix := 2
	for nameInUse(registry, candidate) {
		stem := base
		if len(stem) > 58 {
			stem = stem[:58]
		}
		candidate = stem + "_" + itoa(suffix)
		suffix++
	}
	registry[raw] = candidate
	return candidate
}

func nameInUse(registry map[string]string, candidate string) bool {
	for _, v := range registry {
		if v == candidate {
			return true
		}
	}
	return false
}

func remapNameField(nameVal *any, registry map[string]string) {
	raw, ok := (*nameVal).(string)
	if !ok {
		return
	}
	if mapped, ok := registry[raw]; ok {
		*nameVal = mapped
	} else {
		*nameVal = SanitizeFunctionName(raw)
	}
}

func sanitizeToolChoice(body map[string]any, registry map[string]string) {
	choice, ok := body["tool_choice"]
	if !ok {
		return
	}
	delete(body, "tool_choice")

	switch c := choice.(type) {
	case string:
		if c == "auto" || c == "none" || c == "required" {
			body["tool_choice"] = c
		} else {
			body["tool_choice"] = "auto"
		}
	case map[string]any:
		if stringField(c, "type") == "function" {
			if name, ok := c["name"]; ok {
				delete(c, "name")
				c["function"] = map[string]any{"name": name}
			}
			if fn, ok := mapField(c, "function"); ok {
				if nameVal, ok := fn["name"]; ok {
					n := nameVal
					remapNameField(&n, registry)
					fn["name"] = n
				}
			}
			body["tool_choice"] = c
		} else {
			body["tool_choice"] = "auto"
		}
	default:
		body["tool_choice"] = "auto"
	}
}

func normalizeAndSanitizeTools(body map[string]any, registry map[string]string) {
	tools, ok := body["tools"].([]any)
	if !ok {
		return
	}
	var kept []any
	for i, raw := range tools {
		tool, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if normalizeCursorTool(tool) {
			sanitizeTool(tool, registry)
			kept = append(kept, tool)
			tools[i] = tool
		}
	}
	body["tools"] = kept
}

func normalizeCursorTool(tool map[string]any) bool {
	toolType := stringField(tool, "type")
	if toolType == "" {
		toolType = "function"
	}
	switch toolType {
	case "function":
		if _, has := tool["function"]; !has {
			fn := map[string]any{}
			if name, ok := tool["name"]; ok {
				fn["name"] = name
				delete(tool, "name")
			}
			if desc, ok := tool["description"]; ok {
				fn["description"] = desc
				delete(tool, "description")
			}
			if params, ok := tool["parameters"]; ok {
				fn["parameters"] = params
				delete(tool, "parameters")
			} else {
				fn["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
			}
			if strict, ok := tool["strict"]; ok {
				fn["strict"] = strict
				delete(tool, "strict")
			}
			tool["function"] = fn
		}
		tool["type"] = "function"
		return true
	case "custom":
		name := stringField(tool, "name")
		if name == "" {
			name = "custom_tool"
		}
		desc := stringField(tool, "description")
		if desc == "" {
			desc = "Custom Cursor tool"
		}
		for k := range tool {
			delete(tool, k)
		}
		tool["type"] = "function"
		tool["function"] = map[string]any{
			"name":        name,
			"description": desc,
			"parameters":  map[string]any{"type": "object", "properties": map[string]any{}},
		}
		return true
	default:
		return false
	}
}

func sanitizeTool(tool map[string]any, registry map[string]string) {
	if stringField(tool, "type") != "function" {
		return
	}
	fn, ok := mapField(tool, "function")
	if !ok {
		return
	}
	if name := stringField(fn, "name"); name != "" {
		fn["name"] = uniqueFunctionName(name, registry)
	} else {
		fn["name"] = uniqueFunctionName("unnamed_tool", registry)
	}
	delete(fn, "strict")
	if _, has := fn["parameters"]; !has {
		fn["parameters"] = map[string]any{"type": "object", "properties": map[string]any{}}
	}
	if params, ok := fn["parameters"]; ok {
		if pm, ok := params.(map[string]any); ok {
			SanitizeSchema(pm)
			fn["parameters"] = pm
		}
	}
}

// SanitizeSchema normalizes tool parameters JSON Schema for upstream MFJS.
func SanitizeSchema(value map[string]any) {
	normalizeSchemaNode(value, 0)
	if !hasCombinator(value) && stringField(value, "type") == "" {
		value["type"] = "object"
	}
}

func hasCombinator(obj map[string]any) bool {
	_, a := obj["anyOf"]
	_, o := obj["oneOf"]
	_, al := obj["allOf"]
	return a || o || al
}

func inferTypeFromValue(v any) string {
	switch val := v.(type) {
	case bool:
		return "boolean"
	case float64:
		if val == float64(int64(val)) {
			return "integer"
		}
		return "number"
	case int, int64:
		return "integer"
	case string:
		return "string"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "string"
	}
}

func ensureType(obj map[string]any) {
	if stringField(obj, "type") != "" || hasCombinator(obj) {
		return
	}
	if _, has := obj["$ref"]; has {
		return
	}
	inferred := "string"
	if enumVals, ok := obj["enum"].([]any); ok && len(enumVals) > 0 {
		inferred = inferTypeFromValue(enumVals[0])
	} else if c, ok := obj["const"]; ok {
		inferred = inferTypeFromValue(c)
	} else if _, ok := obj["properties"]; ok {
		inferred = "object"
	} else if _, ok := obj["items"]; ok {
		inferred = "array"
	}
	obj["type"] = inferred
}

func collapseDeepNode(obj map[string]any) {
	collapsedType := stringField(obj, "type")
	if collapsedType == "" {
		if _, ok := obj["properties"]; ok || hasCombinator(obj) {
			collapsedType = "object"
		} else if _, ok := obj["items"]; ok {
			collapsedType = "array"
		} else {
			collapsedType = "string"
		}
	}
	desc := stringField(obj, "description")
	for k := range obj {
		delete(obj, k)
	}
	obj["type"] = collapsedType
	if desc != "" {
		obj["description"] = desc
	}
}

func normalizeSchemaNode(value map[string]any, depth int) {
	if definitions, ok := value["definitions"].(map[string]any); ok {
		delete(value, "definitions")
		defs, _ := value["$defs"].(map[string]any)
		if defs == nil {
			defs = map[string]any{}
			value["$defs"] = defs
		}
		for k, v := range definitions {
			if _, exists := defs[k]; !exists {
				defs[k] = v
			}
		}
	}

	if ref, ok := value["$ref"].(string); ok {
		fixed := strings.ReplaceAll(ref, "#/definitions/", "#/$defs/")
		if strings.HasPrefix(fixed, "#/$defs/") {
			value["$ref"] = fixed
		} else {
			delete(value, "$ref")
		}
	}

	delete(value, "$schema")
	delete(value, "strict")
	delete(value, "additionalProperties")
	delete(value, "unevaluatedProperties")

	if depth >= maxSchemaDepth {
		collapseDeepNode(value)
		return
	}

	if hasCombinator(value) {
		delete(value, "type")
		for _, key := range []string{"anyOf", "oneOf", "allOf"} {
			if items, ok := value[key].([]any); ok {
				for i, item := range items {
					if child, ok := item.(map[string]any); ok {
						normalizeSchemaNode(child, depth+1)
						ensureType(child)
						items[i] = child
					}
				}
				value[key] = items
			}
		}
		return
	}

	ensureType(value)

	if props, ok := value["properties"].(map[string]any); ok {
		for k, prop := range props {
			if child, ok := prop.(map[string]any); ok {
				normalizeSchemaNode(child, depth+1)
				ensureType(child)
				props[k] = child
			}
		}
	}

	if items, ok := value["items"]; ok {
		switch it := items.(type) {
		case map[string]any:
			normalizeSchemaNode(it, depth+1)
			ensureType(it)
			value["items"] = it
		case []any:
			for i, item := range it {
				if child, ok := item.(map[string]any); ok {
					normalizeSchemaNode(child, depth+1)
					ensureType(child)
					it[i] = child
				}
			}
			value["items"] = it
		}
	}

	if defs, ok := value["$defs"].(map[string]any); ok {
		for k, def := range defs {
			if child, ok := def.(map[string]any); ok {
				normalizeSchemaNode(child, depth+1)
				defs[k] = child
			}
		}
	}
}
