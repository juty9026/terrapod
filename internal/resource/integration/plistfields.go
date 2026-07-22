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
