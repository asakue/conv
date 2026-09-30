package codec

import (
	"bytes"
	"fmt"
	"io"
	"math"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/gurzi/conv/internal/value"
)

// YAMLCodec реализует формат YAML (реализация gopkg.in/yaml.v3).
type YAMLCodec struct {
	// Indent — отступ в пробелах (0 → значение по умолчанию yaml.v3 = 4).
	Indent int
	// Compact — компактный вывод (flow-стиль для вложенных коллекций).
	Compact bool
}

// NewYAML возвращает кодек YAML с отступом в два пробела.
func NewYAML() *YAMLCodec { return &YAMLCodec{Indent: 2} }

// Decode читает YAML из r. Поддерживается единственный документ
// (первый в потоке).
func (YAMLCodec) Decode(r io.Reader) (value.Value, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return value.Value{}, fmt.Errorf("yaml: %w", err)
	}
	return DecodeYAMLBytes(data)
}

// DecodeYAMLBytes разбирает байты YAML.
func DecodeYAMLBytes(data []byte) (value.Value, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return value.NewNil(), nil
	}
	var raw any
	dec := yaml.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&raw); err != nil {
		if err.Error() == "EOF" {
			return value.NewNil(), nil
		}
		return value.Value{}, fmt.Errorf("yaml: %w", err)
	}
	v, err := fromYAMLAny(raw)
	if err != nil {
		return value.Value{}, fmt.Errorf("yaml: %w", err)
	}
	return v, nil
}

// fromYAMLAny конвертирует результат yaml.Unmarshal в Value.
// yaml.v3 возвращает map[string]interface{}, когда все ключи — строки,
// и map[interface{}]interface{} в остальных случаях.
func fromYAMLAny(raw any) (value.Value, error) {
	switch t := raw.(type) {
	case nil:
		return value.NewNil(), nil
	case bool:
		return value.NewBool(t), nil
	case string:
		return value.NewString(t), nil
	case int:
		return value.NewInt(int64(t)), nil
	case int64:
		return value.NewInt(t), nil
	case uint64:
		return value.NewInt(int64(t)), nil
	case float64:
		return value.NewFloat(t), nil
	case []any:
		out := make([]value.Value, len(t))
		for i, e := range t {
			ev, err := fromYAMLAny(e)
			if err != nil {
				return value.Value{}, err
			}
			out[i] = ev
		}
		return value.NewArray(out), nil
	case map[string]any:
		out := make(map[string]value.Value, len(t))
		for k, e := range t {
			ev, err := fromYAMLAny(e)
			if err != nil {
				return value.Value{}, err
			}
			out[k] = ev
		}
		return value.NewMap(out), nil
	case map[any]any:
		out := make(map[string]value.Value, len(t))
		for k, e := range t {
			ev, err := fromYAMLAny(e)
			if err != nil {
				return value.Value{}, err
			}
			out[fmt.Sprintf("%v", k)] = ev
		}
		return value.NewMap(out), nil
	default:
		return value.Value{}, fmt.Errorf("неподдерживаемый yaml-тип %T", raw)
	}
}

// Encode записывает значение в формате YAML.
func (c YAMLCodec) Encode(w io.Writer, v value.Value) error {
	doc := yamlDoc(v)
	enc := yaml.NewEncoder(w)
	if c.Indent > 0 {
		enc.SetIndent(c.Indent)
	}
	if err := enc.Encode(sortedDoc(doc)); err != nil {
		return fmt.Errorf("yaml: %w", err)
	}
	return enc.Close()
}

// yamlDoc готовит значение к маршалингу yaml.v3.
// Карты представляются как yaml.MapSlice-подобная структура для
// детерминированного порядка ключей.
func yamlDoc(v value.Value) any {
	switch v.Type {
	case value.TypeMap:
		m := v.MapVal()
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range keys {
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
				yamlNode(m[k]),
			)
		}
		return node
	case value.TypeArray:
		return yamlNode(v)
	default:
		return yamlNode(v)
	}
}

func yamlNode(v value.Value) *yaml.Node {
	switch v.Type {
	case value.TypeNil:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}
	case value.TypeBool:
		b, _ := v.Value.(bool)
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: strconv.FormatBool(b)}
	case value.TypeInt:
		n, _ := v.Value.(int64)
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: strconv.FormatInt(n, 10)}
	case value.TypeFloat:
		f, _ := v.Value.(float64)
		s := strconv.FormatFloat(f, 'g', -1, 64)
		if math.IsInf(f, 1) {
			s = ".inf"
		} else if math.IsInf(f, -1) {
			s = "-.inf"
		} else if math.IsNaN(f) {
			s = ".nan"
		} else if !strings.ContainsAny(s, ".eE") {
			s += ".0"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!float", Value: s}
	case value.TypeString:
		s, _ := v.Value.(string)
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: s}
	case value.TypeArray:
		node := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, e := range v.ArrayVal() {
			node.Content = append(node.Content, yamlNode(e))
		}
		return node
	case value.TypeMap:
		m := v.MapVal()
		keys := make([]string, 0, len(m))
		for k := range m {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, k := range keys {
			node.Content = append(node.Content,
				&yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: k},
				yamlNode(m[k]),
			)
		}
		return node
	default:
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: v.String()}
	}
}

// sortedDoc нужен, чтобы верхний уровень карты тоже кодировался узлом.
func sortedDoc(doc any) any { return doc }
