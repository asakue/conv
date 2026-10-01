package codec

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/asakue/conv/internal/value"
)

// XMLCodec преобразует XML в древовидную структуру и обратно.
//
// Правила отображения (симметричны при чтении и записи):
//   - корневой элемент становится верхним ключом документа;
//   - вложенные элементы — ключами карты;
//   - повторяющиеся соседи с одинаковым именем — массивом;
//   - атрибуты — ключами с префиксом "@";
//   - смешанное содержимое (текст рядом с детьми) — ключом "#text".
type XMLCodec struct {
	// Indent — строка отступа (по умолчанию два пробела).
	Indent int
	// Root — имя корневого элемента при записи (по умолчанию "root").
	Root string
}

// Decode читает XML из r.
func (c XMLCodec) Decode(r io.Reader) (value.Value, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return value.Value{}, fmt.Errorf("xml: %w", err)
	}
	return c.DecodeBytes(data)
}

// DecodeBytes разбирает байты XML.
func (c XMLCodec) DecodeBytes(data []byte) (value.Value, error) {
	dec := xml.NewDecoder(bytes.NewReader(data))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return value.Value{}, fmt.Errorf("xml: документ не содержит элементов")
		}
		if err != nil {
			return value.Value{}, fmt.Errorf("xml: %w", err)
		}
		se, ok := tok.(xml.StartElement)
		if !ok {
			continue
		}
		inner, err := c.readElement(dec, se)
		if err != nil {
			return value.Value{}, err
		}
		name := xmlName(se.Name.Local)
		return value.NewMap(map[string]value.Value{name: inner}), nil
	}
}

func (c XMLCodec) readElement(dec *xml.Decoder, se xml.StartElement) (value.Value, error) {
	attrs := make(map[string]any)
	for _, a := range se.Attr {
		attrs[xmlAttrPrefix+xmlName(a.Name.Local)] = xmlInfer(a.Value)
	}

	children := make(map[string]any)
	var text strings.Builder

	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return value.Value{}, fmt.Errorf("xml: незакрытый элемент <%s>", se.Name.Local)
		}
		if err != nil {
			return value.Value{}, fmt.Errorf("xml: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			child, err := c.readElement(dec, t)
			if err != nil {
				return value.Value{}, err
			}
			name := xmlName(t.Name.Local)
			if prev, seen := children[name]; seen {
				if list, isList := prev.([]value.Value); isList {
					children[name] = append(list, child)
				} else if pv, isVal := prev.(value.Value); isVal {
					children[name] = []value.Value{pv, child}
				} else {
					// Неожиданный тип — конвертируем.
					children[name] = []value.Value{toValue(prev), child}
				}
			} else {
				children[name] = child
			}
		case xml.CharData:
			text.Write(t)
		case xml.EndElement:
			inner := strings.TrimSpace(text.String())
			if len(children) == 0 && len(attrs) == 0 {
				if inner == "" {
					return value.NewNil(), nil
				}
				return value.NewString(inner), nil
			}
			out := make(map[string]any, len(children)+len(attrs)+1)
			for k, v := range children {
				out[k] = v
			}
			for k, v := range attrs {
				out[k] = v
			}
			if inner != "" {
				out[xmlTextKey] = xmlInfer(inner)
			}
			return toValue(out), nil
		}
	}
}

// Encode записывает значение в формате XML.
func (c XMLCodec) Encode(w io.Writer, v value.Value) error {
	indent := "  "
	if c.Indent > 0 {
		indent = strings.Repeat(" ", c.Indent)
	}
	root := strings.TrimSpace(c.Root)
	if root == "" {
		root = "root"
	}
	var b strings.Builder
	b.WriteString(xml.Header + "\n")
	if err := c.writeElement(&b, xmlName(root), v, indent, 0); err != nil {
		return err
	}
	_, err := w.Write([]byte(b.String() + "\n"))
	return err
}

func (c XMLCodec) writeElement(b *strings.Builder, name string, v value.Value, indent string, depth int) error {
	pad := strings.Repeat(indent, depth)

	switch v.Type {
	case value.TypeNil:
		b.WriteString(pad + "<" + name + "/>")
		return nil

	case value.TypeArray:
		items := v.ArrayVal()
		if len(items) == 0 {
			b.WriteString(pad + "<" + name + "/>")
			return nil
		}
		for _, item := range items {
			if err := c.writeElement(b, name, item, indent, depth); err != nil {
				return err
			}
			b.WriteString("\n")
		}
		return nil

	case value.TypeMap:
		return c.writeMapElement(b, name, v.MapVal(), indent, depth, pad)

	default:
		text := v.ScalarString()
		var sb strings.Builder
		xml.EscapeText(&sb, []byte(text))
		b.WriteString(pad + "<" + name + ">" + sb.String() + "</" + name + ">")
		return nil
	}
}

func (c XMLCodec) writeMapElement(b *strings.Builder, name string, m map[string]value.Value, indent string, depth int, pad string) error {
	attrs := make([]string, 0, len(m))
	children := make([]string, 0, len(m))
	text := ""
	hasText := false

	for k, v := range m {
		switch {
		case k == xmlTextKey:
			text = v.ScalarString()
			hasText = true
		case strings.HasPrefix(k, xmlAttrPrefix):
			t, err := xmlScalarText(v)
			if err != nil {
				return fmt.Errorf("xml: атрибут %q: %w", k, err)
			}
			var sb strings.Builder
			sb.WriteString(" " + xmlName(strings.TrimPrefix(k, xmlAttrPrefix)) + `="`)
			xml.EscapeText(&sb, []byte(t))
			sb.WriteString(`"`)
			attrs = append(attrs, sb.String())
		default:
			children = append(children, k)
		}
	}
	sort.Strings(attrs)
	sort.Strings(children)

	if len(children) == 0 && len(attrs) == 0 && !hasText {
		b.WriteString(pad + "<" + name + "/>")
		return nil
	}

	tag := "<" + name + strings.Join(attrs, "") + ">"
	if hasText && len(children) == 0 {
		var sb strings.Builder
		xml.EscapeText(&sb, []byte(text))
		b.WriteString(pad + tag + sb.String() + "</" + name + ">")
		return nil
	}

	b.WriteString(pad + tag + "\n")
	if hasText {
		var sb strings.Builder
		xml.EscapeText(&sb, []byte(text))
		b.WriteString(strings.Repeat(indent, depth+1) + sb.String() + "\n")
	}
	for _, k := range children {
		if v, ok := m[k]; ok {
			if err := c.writeElement(b, xmlName(k), v, indent, depth+1); err != nil {
				return err
			}
		}
		b.WriteString("\n")
	}
	b.WriteString(pad + "</" + name + ">")
	return nil
}

// xmlScalarText строит текстовое представление скаляра.
func xmlScalarText(v value.Value) (string, error) {
	switch v.Type {
	case value.TypeNil:
		return "", nil
	case value.TypeString, value.TypeInt, value.TypeFloat, value.TypeBool:
		return v.ScalarString(), nil
	default:
		return "", fmt.Errorf("неподдерживаемый тип %v", v.Type)
	}
}

// xmlInfer пытается распознать тип строки.
func xmlInfer(s string) any {
	switch strings.ToLower(s) {
	case "true":
		return true
	case "false":
		return false
	case "null", "nil", "~":
		return nil
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return i
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return f
	}
	return s
}

// toValue конвертирует map[string]any в value.Value.
func toValue(v any) value.Value {
	switch tv := v.(type) {
	case nil:
		return value.NewNil()
	case bool:
		return value.NewBool(tv)
	case int:
		return value.NewInt(int64(tv))
	case int64:
		return value.NewInt(tv)
	case float64:
		return value.NewFloat(tv)
	case string:
		return value.NewString(tv)
	case []value.Value:
		return value.NewArray(tv)
	case map[string]any:
		m := make(map[string]value.Value, len(tv))
		for k, v := range tv {
			m[k] = toValue(v)
		}
		return value.NewMap(m)
	default:
		return value.NewString(fmt.Sprint(v))
	}
}

// xmlName превращает произвольную строку в допустимое имя XML-элемента.
func xmlName(s string) string {
	var b strings.Builder
	for i, r := range s {
		switch {
		case r == '_' || r == '-' || r == '.' || r == ':' ||
			(r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
			(i > 0 && r >= '0' && r <= '9'):
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	out := b.String()
	if out == "" {
		return "item"
	}
	if c := out[0]; (c >= '0' && c <= '9') || c == '-' || c == '.' {
		out = "_" + out
	}
	return out
}

const (
	xmlAttrPrefix = "@"
	xmlTextKey    = "#text"
)
