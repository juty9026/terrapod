package integration

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

func decodePlist(contents []byte) (map[string]any, error) {
	decoder := xml.NewDecoder(bytes.NewReader(contents))
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		start, ok := token.(xml.StartElement)
		if !ok || start.Name.Local != "plist" {
			continue
		}
		value, err := plistNextValue(decoder)
		if err != nil {
			return nil, err
		}
		root, ok := value.(map[string]any)
		if !ok {
			return nil, errors.New("plist root must be a dict")
		}
		closed := false
		for {
			token, err := decoder.Token()
			if errors.Is(err, io.EOF) {
				if !closed {
					return nil, errors.New("unterminated plist")
				}
				return root, nil
			}
			if err != nil {
				return nil, err
			}
			switch token := token.(type) {
			case xml.EndElement:
				if token.Name.Local != "plist" || closed {
					return nil, errors.New("invalid plist closing element")
				}
				closed = true
			case xml.CharData:
				if strings.TrimSpace(string(token)) != "" {
					return nil, errors.New("unexpected plist text")
				}
			default:
				return nil, errors.New("unexpected content after plist root")
			}
		}
	}
}

func plistNextValue(decoder *xml.Decoder) (any, error) {
	for {
		token, err := decoder.Token()
		if err != nil {
			return nil, err
		}
		if start, ok := token.(xml.StartElement); ok {
			return plistValue(decoder, start)
		}
	}
}

func plistValue(decoder *xml.Decoder, start xml.StartElement) (any, error) {
	switch start.Name.Local {
	case "dict":
		result := make(map[string]any)
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			if end, ok := token.(xml.EndElement); ok && end.Name.Local == "dict" {
				return result, nil
			}
			if chars, ok := token.(xml.CharData); ok && strings.TrimSpace(string(chars)) == "" {
				continue
			}
			if _, ok := token.(xml.Comment); ok {
				continue
			}
			keyStart, ok := token.(xml.StartElement)
			if !ok || keyStart.Name.Local != "key" {
				return nil, errors.New("plist dict contains a non-key")
			}
			var key string
			if err := decoder.DecodeElement(&key, &keyStart); err != nil {
				return nil, err
			}
			if _, duplicate := result[key]; duplicate {
				return nil, fmt.Errorf("duplicate plist key %q", key)
			}
			value, err := plistNextValue(decoder)
			if err != nil {
				return nil, err
			}
			result[key] = value
		}
	case "array":
		var values []any
		for {
			token, err := decoder.Token()
			if err != nil {
				return nil, err
			}
			if end, ok := token.(xml.EndElement); ok && end.Name.Local == "array" {
				return values, nil
			}
			child, ok := token.(xml.StartElement)
			if !ok {
				continue
			}
			value, err := plistValue(decoder, child)
			if err != nil {
				return nil, err
			}
			values = append(values, value)
		}
	case "true":
		if err := decoder.Skip(); err != nil {
			return nil, err
		}
		return true, nil
	case "false":
		if err := decoder.Skip(); err != nil {
			return nil, err
		}
		return false, nil
	case "string", "data", "date", "integer", "real":
		var raw string
		if err := decoder.DecodeElement(&raw, &start); err != nil {
			return nil, err
		}
		switch start.Name.Local {
		case "string":
			return raw, nil
		case "data":
			return base64.StdEncoding.DecodeString(strings.Join(strings.Fields(raw), ""))
		case "date":
			return time.Parse(time.RFC3339, strings.TrimSpace(raw))
		case "integer":
			return strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		default:
			return strconv.ParseFloat(strings.TrimSpace(raw), 64)
		}
	default:
		return nil, fmt.Errorf("unsupported plist value %q", start.Name.Local)
	}
}

func encodePlist(root map[string]any) ([]byte, error) {
	var out bytes.Buffer
	out.WriteString("<?xml version=\"1.0\" encoding=\"UTF-8\"?>\n<!DOCTYPE plist PUBLIC \"-//Apple//DTD PLIST 1.0//EN\" \"http://www.apple.com/DTDs/PropertyList-1.0.dtd\">\n<plist version=\"1.0\">\n")
	encoder := xml.NewEncoder(&out)
	encoder.Indent("", "  ")
	if err := writePlistValue(encoder, root); err != nil {
		return nil, err
	}
	if err := encoder.Flush(); err != nil {
		return nil, err
	}
	out.WriteString("\n</plist>\n")
	return out.Bytes(), nil
}

func writePlistValue(e *xml.Encoder, value any) error {
	switch v := value.(type) {
	case map[string]any:
		start := xml.StartElement{Name: xml.Name{Local: "dict"}}
		if err := e.EncodeToken(start); err != nil {
			return err
		}
		keys := sortedKeys(v)
		for _, key := range keys {
			if err := e.EncodeElement(key, xml.StartElement{Name: xml.Name{Local: "key"}}); err != nil {
				return err
			}
			if err := writePlistValue(e, v[key]); err != nil {
				return err
			}
		}
		return e.EncodeToken(start.End())
	case []any:
		start := xml.StartElement{Name: xml.Name{Local: "array"}}
		if err := e.EncodeToken(start); err != nil {
			return err
		}
		for _, child := range v {
			if err := writePlistValue(e, child); err != nil {
				return err
			}
		}
		return e.EncodeToken(start.End())
	case string:
		return e.EncodeElement(v, xml.StartElement{Name: xml.Name{Local: "string"}})
	case bool:
		name := "false"
		if v {
			name = "true"
		}
		start := xml.StartElement{Name: xml.Name{Local: name}}
		if err := e.EncodeToken(start); err != nil {
			return err
		}
		return e.EncodeToken(start.End())
	case json.Number:
		return e.EncodeElement(string(v), xml.StartElement{Name: xml.Name{Local: "integer"}})
	case int64:
		return e.EncodeElement(strconv.FormatInt(v, 10), xml.StartElement{Name: xml.Name{Local: "integer"}})
	case float64:
		return e.EncodeElement(strconv.FormatFloat(v, 'g', -1, 64), xml.StartElement{Name: xml.Name{Local: "real"}})
	case []byte:
		return e.EncodeElement(base64.StdEncoding.EncodeToString(v), xml.StartElement{Name: xml.Name{Local: "data"}})
	case time.Time:
		return e.EncodeElement(v.UTC().Format(time.RFC3339), xml.StartElement{Name: xml.Name{Local: "date"}})
	case nil:
		return errors.New("nil is not a plist value")
	default:
		return fmt.Errorf("unsupported plist type %T", value)
	}
}

func sortedKeys(value map[string]any) []string {
	keys := make([]string, 0, len(value))
	for key := range value {
		keys = append(keys, key)
	}
	sortStrings(keys)
	return keys
}
func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j] < values[j-1]; j-- {
			values[j], values[j-1] = values[j-1], values[j]
		}
	}
}

type plistSpanDocument struct {
	text string
	root *plistSpanNode
}
type plistSpanNode struct {
	kind                   string
	start, end, closeStart int
	value                  any
	members                map[string]plistSpanMember
}
type plistSpanMember struct {
	keyStart, keyEnd int
	value            *plistSpanNode
}

func parsePlistSpans(text string) (*plistSpanDocument, error) {
	d := xml.NewDecoder(strings.NewReader(text))
	token, start, _, err := nextPlistRootToken(d)
	if err != nil {
		return nil, err
	}
	plist, ok := token.(xml.StartElement)
	if !ok || plist.Name.Local != "plist" {
		return nil, errors.New("plist root element is required")
	}
	token, start, _, err = nextPlistToken(d)
	if err != nil {
		return nil, err
	}
	valueStart, ok := token.(xml.StartElement)
	if !ok {
		return nil, errors.New("plist value is required")
	}
	root, err := parsePlistSpanValue(d, text, valueStart, start)
	if err != nil {
		return nil, err
	}
	if root.kind != "dict" {
		return nil, errors.New("plist root must be a dict")
	}
	token, _, _, err = nextPlistToken(d)
	if err != nil {
		return nil, err
	}
	end, ok := token.(xml.EndElement)
	if !ok || end.Name.Local != "plist" {
		return nil, errors.New("plist closing element is required")
	}
	if token, _, _, err = nextPlistToken(d); !errors.Is(err, io.EOF) {
		if err == nil {
			_ = token
			err = errors.New("unexpected content after plist")
		}
		return nil, err
	}
	return &plistSpanDocument{text: text, root: root}, nil
}

func nextPlistToken(d *xml.Decoder) (xml.Token, int, int, error) {
	for {
		start := int(d.InputOffset())
		token, err := d.Token()
		end := int(d.InputOffset())
		if err != nil {
			return nil, start, end, err
		}
		switch value := token.(type) {
		case xml.CharData:
			if strings.TrimSpace(string(value)) != "" {
				return nil, start, end, errors.New("unexpected non-whitespace plist text")
			}
			continue
		case xml.Comment:
			continue
		default:
			return token, start, end, nil
		}
	}
}

func nextPlistRootToken(d *xml.Decoder) (xml.Token, int, int, error) {
	for {
		token, start, end, err := nextPlistToken(d)
		if err != nil {
			return nil, start, end, err
		}
		switch token.(type) {
		case xml.Directive, xml.ProcInst:
			continue
		default:
			return token, start, end, nil
		}
	}
}

func parsePlistSpanValue(d *xml.Decoder, text string, start xml.StartElement, offset int) (*plistSpanNode, error) {
	node := &plistSpanNode{kind: start.Name.Local, start: offset}
	switch start.Name.Local {
	case "dict":
		node.members = make(map[string]plistSpanMember)
		semantic := make(map[string]any)
		for {
			token, tokenStart, tokenEnd, err := nextPlistToken(d)
			if err != nil {
				return nil, err
			}
			if end, ok := token.(xml.EndElement); ok {
				if end.Name.Local != "dict" {
					return nil, errors.New("unexpected plist end element")
				}
				node.closeStart = tokenStart
				node.end = tokenEnd
				node.value = semantic
				return node, nil
			}
			keyStart, ok := token.(xml.StartElement)
			if !ok || keyStart.Name.Local != "key" {
				return nil, errors.New("plist dict contains a non-key")
			}
			key, keyEnd, err := parsePlistText(d, "key")
			if err != nil {
				return nil, err
			}
			if _, duplicate := node.members[key]; duplicate {
				return nil, fmt.Errorf("duplicate plist key %q", key)
			}
			token, valueStart, _, err := nextPlistToken(d)
			if err != nil {
				return nil, err
			}
			valueElement, ok := token.(xml.StartElement)
			if !ok {
				return nil, errors.New("plist key lacks a value")
			}
			value, err := parsePlistSpanValue(d, text, valueElement, valueStart)
			if err != nil {
				return nil, err
			}
			node.members[key] = plistSpanMember{keyStart: tokenStart, keyEnd: keyEnd, value: value}
			semantic[key] = value.value
		}
	case "array":
		var values []any
		for {
			token, tokenStart, tokenEnd, err := nextPlistToken(d)
			if err != nil {
				return nil, err
			}
			if end, ok := token.(xml.EndElement); ok {
				if end.Name.Local != "array" {
					return nil, errors.New("unexpected plist end element")
				}
				node.value = values
				node.closeStart = tokenStart
				node.end = tokenEnd
				return node, nil
			}
			element, ok := token.(xml.StartElement)
			if !ok {
				return nil, errors.New("plist array contains an invalid token")
			}
			child, err := parsePlistSpanValue(d, text, element, tokenStart)
			if err != nil {
				return nil, err
			}
			values = append(values, child.value)
		}
	case "true", "false":
		token, _, endOffset, err := nextPlistToken(d)
		if err != nil {
			return nil, err
		}
		end, ok := token.(xml.EndElement)
		if !ok || end.Name.Local != start.Name.Local {
			return nil, errors.New("invalid plist boolean")
		}
		node.end = endOffset
		node.value = start.Name.Local == "true"
		return node, nil
	case "string", "data", "date", "integer", "real":
		raw, endOffset, err := parsePlistText(d, start.Name.Local)
		if err != nil {
			return nil, err
		}
		node.end = endOffset
		switch start.Name.Local {
		case "string":
			node.value = raw
		case "data":
			node.value, err = base64.StdEncoding.DecodeString(strings.Join(strings.Fields(raw), ""))
		case "date":
			node.value, err = time.Parse(time.RFC3339, strings.TrimSpace(raw))
		case "integer":
			node.value, err = strconv.ParseInt(strings.TrimSpace(raw), 10, 64)
		case "real":
			node.value, err = strconv.ParseFloat(strings.TrimSpace(raw), 64)
		}
		if err != nil {
			return nil, err
		}
		return node, nil
	default:
		return nil, fmt.Errorf("unsupported plist value %q", start.Name.Local)
	}
}

func parsePlistText(d *xml.Decoder, name string) (string, int, error) {
	var value strings.Builder
	for {
		token, err := d.Token()
		end := int(d.InputOffset())
		if err != nil {
			return "", end, err
		}
		switch token := token.(type) {
		case xml.CharData:
			value.Write([]byte(token))
		case xml.Comment:
			continue
		case xml.EndElement:
			if token.Name.Local != name {
				return "", end, errors.New("unexpected plist end element")
			}
			return value.String(), end, nil
		default:
			return "", end, errors.New("unexpected token inside plist scalar")
		}
	}
}

func (d *plistSpanDocument) get(pointer string) (fieldValue, error) {
	node, _, found, err := d.find(pointer)
	if err != nil || !found {
		return fieldValue{}, err
	}
	return fieldValue{Exists: true, Value: node.value, Raw: []byte(d.text[node.start:node.end])}, nil
}
func (d *plistSpanDocument) find(pointer string) (*plistSpanNode, plistSpanMember, bool, error) {
	parts, err := pointerParts(pointer)
	if err != nil {
		return nil, plistSpanMember{}, false, err
	}
	current := d.root
	var member plistSpanMember
	for i, part := range parts {
		if current.kind != "dict" {
			return nil, member, false, fmt.Errorf("field %s must be an object", "/"+strings.Join(parts[:i], "/"))
		}
		var ok bool
		member, ok = current.members[part]
		if !ok {
			return current, member, false, nil
		}
		current = member.value
	}
	return current, member, true, nil
}
func (d *plistSpanDocument) set(pointer string, value any) (string, error) {
	node, _, found, err := d.find(pointer)
	if err != nil {
		return "", err
	}
	fragment, err := encodePlistFragment(value)
	if err != nil {
		return "", err
	}
	if found {
		return d.text[:node.start] + fragment + d.text[node.end:], nil
	}
	parts, _ := pointerParts(pointer)
	parent := d.root
	for i, part := range parts[:len(parts)-1] {
		member, ok := parent.members[part]
		if !ok {
			nested := value
			for j := len(parts) - 1; j > i; j-- {
				nested = map[string]any{parts[j]: nested}
			}
			fragment, err = encodePlistFragment(nested)
			if err != nil {
				return "", err
			}
			return insertPlistMember(d.text, parent, part, fragment), nil
		}
		if member.value.kind != "dict" {
			return "", fmt.Errorf("field %s must be an object", "/"+strings.Join(parts[:i+1], "/"))
		}
		parent = member.value
	}
	return insertPlistMember(d.text, parent, parts[len(parts)-1], fragment), nil
}
func (d *plistSpanDocument) setRaw(pointer string, raw []byte) (string, error) {
	node, _, found, err := d.find(pointer)
	if err != nil {
		return "", err
	}
	if !found {
		return "", errors.New("plist restore target is absent")
	}
	return d.text[:node.start] + string(raw) + d.text[node.end:], nil
}
func (d *plistSpanDocument) remove(pointer string) (string, error) {
	parts, err := pointerParts(pointer)
	if err != nil {
		return "", err
	}
	text := d.text
	for depth := len(parts); depth > 0; depth-- {
		parsed, err := parsePlistSpans(text)
		if err != nil {
			return "", err
		}
		parent := parsed.root
		for i := 0; i < depth-1; i++ {
			member, ok := parent.members[parts[i]]
			if !ok {
				return text, nil
			}
			parent = member.value
		}
		member, ok := parent.members[parts[depth-1]]
		if !ok {
			return text, nil
		}
		start, end := member.keyStart, member.value.end
		lineStart := strings.LastIndex(text[:start], "\n") + 1
		if strings.TrimSpace(text[lineStart:start]) == "" {
			if relative := strings.IndexByte(text[end:], '\n'); relative >= 0 && strings.TrimSpace(text[end:end+relative]) == "" {
				start = lineStart
				end = end + relative + 1
			}
		}
		text = text[:start] + text[end:]
		if depth == 1 {
			return text, nil
		}
		updated, err := parsePlistSpans(text)
		if err != nil {
			return "", err
		}
		candidate := updated.root
		for i := 0; i < depth-1; i++ {
			candidate = candidate.members[parts[i]].value
		}
		if len(candidate.members) != 0 || strings.TrimSpace(text[indexAfterStartTag(text, candidate.start):candidate.closeStart]) != "" {
			return text, nil
		}
	}
	return text, nil
}
func indexAfterStartTag(text string, start int) int {
	relative := strings.IndexByte(text[start:], '>')
	if relative < 0 {
		return start
	}
	return start + relative + 1
}
func insertPlistMember(text string, parent *plistSpanNode, key, fragment string) string {
	indent := lineIndent(text, parent.closeStart)
	child := indent + "  "
	keyFragment, _ := encodePlistKey(key)
	addition := child + keyFragment + fragment + "\n"
	return text[:parent.closeStart] + addition + text[parent.closeStart:]
}
func encodePlistKey(key string) (string, error) {
	var out bytes.Buffer
	e := xml.NewEncoder(&out)
	err := e.EncodeElement(key, xml.StartElement{Name: xml.Name{Local: "key"}})
	return out.String(), err
}
func encodePlistFragment(value any) (string, error) {
	var out bytes.Buffer
	e := xml.NewEncoder(&out)
	if err := writePlistValue(e, value); err != nil {
		return "", err
	}
	if err := e.Flush(); err != nil {
		return "", err
	}
	return out.String(), nil
}
