package integration

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
)

type fieldValue struct {
	Exists bool
	Value  any
}

func decodeJSON(contents []byte) (map[string]any, error) {
	decoder := json.NewDecoder(bytes.NewReader(contents))
	decoder.UseNumber()
	var value map[string]any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if value == nil {
		return nil, errors.New("root must be an object")
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return nil, errors.New("multiple JSON values")
	}
	return value, nil
}

func encodeJSON(value map[string]any) ([]byte, error) {
	contents, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(contents, '\n'), nil
}

func pointerParts(pointer string) ([]string, error) {
	if pointer == "" || pointer[0] != '/' {
		return nil, fmt.Errorf("invalid field path %q", pointer)
	}
	raw := strings.Split(pointer[1:], "/")
	parts := make([]string, len(raw))
	for i, part := range raw {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		if part == "" {
			return nil, fmt.Errorf("empty field path component in %q", pointer)
		}
		parts[i] = part
	}
	return parts, nil
}

func getField(root map[string]any, pointer string) (fieldValue, error) {
	parts, err := pointerParts(pointer)
	if err != nil {
		return fieldValue{}, err
	}
	current := root
	for i, part := range parts {
		value, ok := current[part]
		if !ok {
			return fieldValue{}, nil
		}
		if i == len(parts)-1 {
			return fieldValue{Exists: true, Value: value}, nil
		}
		next, ok := value.(map[string]any)
		if !ok {
			return fieldValue{}, fmt.Errorf("field %s must be an object", "/"+strings.Join(parts[:i+1], "/"))
		}
		current = next
	}
	return fieldValue{}, nil
}

func setField(root map[string]any, pointer string, value any) error {
	parts, err := pointerParts(pointer)
	if err != nil {
		return err
	}
	current := root
	for i, part := range parts {
		if i == len(parts)-1 {
			current[part] = value
			return nil
		}
		nextValue, ok := current[part]
		if !ok {
			next := make(map[string]any)
			current[part] = next
			current = next
			continue
		}
		next, ok := nextValue.(map[string]any)
		if !ok {
			return fmt.Errorf("field %s must be an object", "/"+strings.Join(parts[:i+1], "/"))
		}
		current = next
	}
	return nil
}

func removeField(root map[string]any, pointer string) error {
	parts, err := pointerParts(pointer)
	if err != nil {
		return err
	}
	objects := []map[string]any{root}
	current := root
	for i, part := range parts {
		if i == len(parts)-1 {
			delete(current, part)
			break
		}
		nextValue, ok := current[part]
		if !ok {
			return nil
		}
		next, ok := nextValue.(map[string]any)
		if !ok {
			return fmt.Errorf("field %s must be an object", "/"+strings.Join(parts[:i+1], "/"))
		}
		current = next
		objects = append(objects, current)
	}
	for i := len(parts) - 2; i >= 0; i-- {
		if len(objects[i+1]) != 0 {
			break
		}
		delete(objects[i], parts[i])
	}
	return nil
}

type jsoncToken struct {
	kind       string
	start, end int
	value      any
}
type jsoncMember struct {
	key   jsoncToken
	value *jsoncNode
}
type jsoncNode struct {
	kind       string
	start, end int
	members    map[string]jsoncMember
	value      any
}

func tokenizeJSONC(text string) ([]jsoncToken, error) {
	var result []jsoncToken
	for i := 0; i < len(text); {
		if strings.ContainsRune(" \t\r\n", rune(text[i])) {
			i++
			continue
		}
		if i+1 < len(text) && text[i:i+2] == "//" {
			if n := strings.IndexByte(text[i+2:], '\n'); n >= 0 {
				i += n + 3
			} else {
				i = len(text)
			}
			continue
		}
		if i+1 < len(text) && text[i:i+2] == "/*" {
			n := strings.Index(text[i+2:], "*/")
			if n < 0 {
				return nil, errors.New("unterminated JSONC block comment")
			}
			i += n + 4
			continue
		}
		if strings.ContainsRune("{}[],: ", rune(text[i])) && text[i] != ' ' {
			result = append(result, jsoncToken{kind: string(text[i]), start: i, end: i + 1})
			i++
			continue
		}
		if text[i] == '"' {
			start := i
			i++
			escaped := false
			for i < len(text) {
				c := text[i]
				i++
				if c == '"' && !escaped {
					break
				}
				if c == '\\' && !escaped {
					escaped = true
				} else {
					escaped = false
				}
			}
			if i > len(text) || text[i-1] != '"' {
				return nil, errors.New("unterminated JSONC string")
			}
			var value string
			if err := json.Unmarshal([]byte(text[start:i]), &value); err != nil {
				return nil, err
			}
			result = append(result, jsoncToken{kind: "string", start: start, end: i, value: value})
			continue
		}
		start := i
		for i < len(text) && !strings.ContainsRune("{}[],:/ \t\r\n", rune(text[i])) {
			i++
		}
		if start == i {
			return nil, fmt.Errorf("unexpected JSONC byte %d", i)
		}
		raw := text[start:i]
		var value any
		decoder := json.NewDecoder(strings.NewReader(raw))
		decoder.UseNumber()
		if err := decoder.Decode(&value); err != nil {
			return nil, err
		}
		result = append(result, jsoncToken{kind: "literal", start: start, end: i, value: value})
	}
	return result, nil
}

func parseJSONC(text string) (*jsoncNode, error) {
	tokens, err := tokenizeJSONC(text)
	if err != nil {
		return nil, err
	}
	if len(tokens) == 0 {
		return nil, errors.New("empty JSONC document")
	}
	node, next, err := parseJSONCNode(tokens, 0)
	if err != nil {
		return nil, err
	}
	if next != len(tokens) || node.kind != "object" {
		return nil, errors.New("JSONC root must contain one object")
	}
	return node, nil
}

func parseJSONCNode(tokens []jsoncToken, at int) (*jsoncNode, int, error) {
	if at >= len(tokens) {
		return nil, at, errors.New("unexpected end of JSONC")
	}
	tok := tokens[at]
	if tok.kind == "{" {
		n := &jsoncNode{kind: "object", start: tok.start, members: make(map[string]jsoncMember)}
		at++
		for at < len(tokens) && tokens[at].kind != "}" {
			key := tokens[at]
			if key.kind != "string" || at+1 >= len(tokens) || tokens[at+1].kind != ":" {
				return nil, at, errors.New("invalid JSONC object member")
			}
			value, next, err := parseJSONCNode(tokens, at+2)
			if err != nil {
				return nil, at, err
			}
			name := key.value.(string)
			if _, duplicate := n.members[name]; duplicate {
				return nil, at, fmt.Errorf("duplicate JSONC field %q", name)
			}
			n.members[name] = jsoncMember{key: key, value: value}
			at = next
			if at < len(tokens) && tokens[at].kind == "," {
				at++
				continue
			}
			if at >= len(tokens) || tokens[at].kind != "}" {
				return nil, at, errors.New("missing JSONC object comma")
			}
		}
		if at >= len(tokens) {
			return nil, at, errors.New("unterminated JSONC object")
		}
		n.end = tokens[at].end
		return n, at + 1, nil
	}
	if tok.kind == "[" {
		i := at + 1
		for i < len(tokens) && tokens[i].kind != "]" {
			_, next, err := parseJSONCNode(tokens, i)
			if err != nil {
				return nil, at, err
			}
			i = next
			if i < len(tokens) && tokens[i].kind == "," {
				i++
				continue
			}
			if i >= len(tokens) || tokens[i].kind != "]" {
				return nil, at, errors.New("missing JSONC array comma")
			}
		}
		if i >= len(tokens) {
			return nil, at, errors.New("unterminated JSONC array")
		}
		return &jsoncNode{kind: "array", start: tok.start, end: tokens[i].end}, i + 1, nil
	}
	if tok.kind == "string" || tok.kind == "literal" {
		return &jsoncNode{kind: tok.kind, start: tok.start, end: tok.end, value: tok.value}, at + 1, nil
	}
	return nil, at, errors.New("invalid JSONC value")
}

func jsoncGet(text, pointer string) (fieldValue, error) {
	root, err := parseJSONC(text)
	if err != nil {
		return fieldValue{}, err
	}
	parts, err := pointerParts(pointer)
	if err != nil {
		return fieldValue{}, err
	}
	current := root
	for i, part := range parts {
		member, ok := current.members[part]
		if !ok {
			return fieldValue{}, nil
		}
		current = member.value
		if i < len(parts)-1 && current.kind != "object" {
			return fieldValue{}, fmt.Errorf("field %s must be an object", "/"+strings.Join(parts[:i+1], "/"))
		}
	}
	if current.kind == "object" || current.kind == "array" {
		value, err := decodeJSONCValue(text[current.start:current.end])
		if err != nil {
			return fieldValue{}, err
		}
		return fieldValue{Exists: true, Value: value}, nil
	}
	return fieldValue{Exists: true, Value: current.value}, nil
}

func decodeJSONCValue(text string) (any, error) {
	tokens, err := tokenizeJSONC(text)
	if err != nil {
		return nil, err
	}
	var normalized strings.Builder
	for i, token := range tokens {
		if token.kind == "," && i+1 < len(tokens) && (tokens[i+1].kind == "}" || tokens[i+1].kind == "]") {
			continue
		}
		normalized.WriteString(text[token.start:token.end])
	}
	decoder := json.NewDecoder(strings.NewReader(normalized.String()))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	return value, nil
}

func jsoncSet(text, pointer string, desired any) (string, error) {
	root, err := parseJSONC(text)
	if err != nil {
		return "", err
	}
	parts, err := pointerParts(pointer)
	if err != nil {
		return "", err
	}
	current := root
	for i, part := range parts {
		member, ok := current.members[part]
		if i == len(parts)-1 {
			rendered, err := json.Marshal(desired)
			if err != nil {
				return "", err
			}
			if ok {
				return text[:member.value.start] + string(rendered) + text[member.value.end:], nil
			}
			return jsoncInsert(text, current, part, string(rendered)), nil
		}
		if ok {
			if member.value.kind != "object" {
				return "", fmt.Errorf("field %s must be an object", "/"+strings.Join(parts[:i+1], "/"))
			}
			current = member.value
			continue
		}
		nested := desired
		for j := len(parts) - 1; j > i; j-- {
			nested = map[string]any{parts[j]: nested}
		}
		rendered, _ := json.MarshalIndent(nested, "", "  ")
		return jsoncInsert(text, current, part, string(rendered)), nil
	}
	return text, nil
}

func jsoncRemove(text, pointer string) (string, error) {
	parts, err := pointerParts(pointer)
	if err != nil {
		return "", err
	}
	for depth := len(parts); depth > 0; depth-- {
		root, err := parseJSONC(text)
		if err != nil {
			return "", err
		}
		parent := root
		for i := 0; i < depth-1; i++ {
			member, ok := parent.members[parts[i]]
			if !ok {
				return text, nil
			}
			if member.value.kind != "object" {
				return "", fmt.Errorf("field %s must be an object", "/"+strings.Join(parts[:i+1], "/"))
			}
			parent = member.value
		}
		member, ok := parent.members[parts[depth-1]]
		if !ok {
			return text, nil
		}
		text, err = jsoncDeleteMember(text, parent, member)
		if err != nil {
			return "", err
		}
		if depth == 1 {
			return text, nil
		}
		updated, err := parseJSONC(text)
		if err != nil {
			return "", err
		}
		candidate := updated
		for i := 0; i < depth-1; i++ {
			candidate = candidate.members[parts[i]].value
		}
		if len(candidate.members) != 0 || strings.TrimSpace(text[candidate.start+1:candidate.end-1]) != "" {
			return text, nil
		}
	}
	return text, nil
}

func jsoncDeleteMember(text string, object *jsoncNode, member jsoncMember) (string, error) {
	tokens, err := tokenizeJSONC(text[object.start+1 : object.end-1])
	if err != nil {
		return "", err
	}
	offset := object.start + 1
	keyIndex := -1
	for i, token := range tokens {
		if token.start+offset == member.key.start {
			keyIndex = i
			break
		}
	}
	if keyIndex < 0 {
		return "", errors.New("JSONC member token is missing")
	}
	valueEndIndex := keyIndex
	for i := keyIndex; i < len(tokens); i++ {
		if tokens[i].end+offset == member.value.end {
			valueEndIndex = i
		}
	}
	start, end := member.key.start, member.value.end
	if valueEndIndex+1 < len(tokens) && tokens[valueEndIndex+1].kind == "," {
		end = tokens[valueEndIndex+1].end + offset
	} else if keyIndex > 0 && tokens[keyIndex-1].kind == "," {
		start = tokens[keyIndex-1].start + offset
	}
	return text[:start] + text[end:], nil
}

func jsoncInsert(text string, object *jsoncNode, key, rendered string) string {
	close := object.end - 1
	indent := lineIndent(text, close)
	child := indent + "  "
	prefix := ""
	if len(object.members) > 0 {
		lastEnd := object.start + 1
		for _, member := range object.members {
			if member.value.end > lastEnd {
				lastEnd = member.value.end
			}
		}
		between, _ := tokenizeJSONC(text[lastEnd:close])
		hasTrailingComma := false
		for _, token := range between {
			hasTrailingComma = hasTrailingComma || token.kind == ","
		}
		if !hasTrailingComma {
			prefix = ","
		}
	}
	if close > 0 && text[close-1] == '\n' {
		return text[:close-1] + prefix + "\n" + child + strconv.Quote(key) + ": " + rendered + "\n" + indent + text[close:]
	}
	return text[:close] + prefix + "\n" + child + strconv.Quote(key) + ": " + rendered + "\n" + indent + text[close:]
}

func lineIndent(text string, at int) string {
	start := strings.LastIndex(text[:at], "\n") + 1
	i := start
	for i < at && (text[i] == ' ' || text[i] == '\t') {
		i++
	}
	return text[start:i]
}
