package codec

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"time"

	"github.com/BurntSushi/toml"

	"github.com/gurzi/conv/internal/value"
)

// TOMLCodec реализует формат TOML v1.0 (github.com/BurntSushi/toml).
type TOMLCodec struct{}

// NewTOML возвращает кодек TOML.
func NewTOML() *TOMLCodec { return &TOMLCodec{} }

// Decode читает TOML из r.
func (TOMLCodec) Decode(r io.Reader) (value.Value, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return value.Value{}, fmt.Errorf("toml: %w", err)
	}
	return DecodeTOMLBytes(data)
}

// DecodeTOMLBytes разбирает байты TOML.
func DecodeTOMLBytes(data []byte) (value.Value, error) {
	if len(bytes.TrimSpace(data)) == 0 {
		return value.NewNil(), nil
	}
	var raw map[string]any
	md, err := toml.Decode(string(data), &raw)
	if err != nil {
		return value.Value{}, fmt.Errorf("toml: %w", err)
	}
	if len(md.Undecoded()) > 0 {
		return value.Value{}, fmt.Errorf("toml: не разобраны ключи: %v", md.Undecoded())
	}
	v, err := fromTOMLAny(raw)
	if err != nil {
		return value.Value{}, fmt.Errorf("toml: %w", err)
	}
	return v, nil
}

// fromTOMLAny конвертирует результат BurntSushi/toml в Value.
func fromTOMLAny(raw any) (value.Value, error) {
	switch t := raw.(type) {
	case nil:
		return value.NewNil(), nil
	case bool:
		return value.NewBool(t), nil
	case string:
		return value.NewString(t), nil
	case int64:
		return value.NewInt(t), nil
	case float64:
		return value.NewFloat(t), nil
	case time.Time:
		return value.NewTime(t), nil
	case []any:
		out := make([]value.Value, len(t))
		for i, e := range t {
			ev, err := fromTOMLAny(e)
			if err != nil {
				return value.Value{}, err
			}
			out[i] = ev
		}
		return value.NewArray(out), nil
	case map[string]any:
		out := make(map[string]value.Value, len(t))
		for k, e := range t {
			ev, err := fromTOMLAny(e)
			if err != nil {
				return value.Value{}, err
			}
			out[k] = ev
		}
		return value.NewMap(out), nil
	default:
		return value.Value{}, fmt.Errorf("неподдерживаемый toml-тип %T", raw)
	}
}

// Encode записывает значение в формате TOML.
//
// Ограничения TOML, обрабатываемые кодеком:
//   - null не поддерживается форматом — такие поля пропускаются;
//   - корнем документа обязана быть карта (скаляр/массив обёртываются
//     в ключ "value");
//   - массивы смешанных типов сериализуются как есть, вложенные массивы
//     массивов TOML не поддерживает.
func (TOMLCodec) Encode(w io.Writer, v value.Value) error {
	var root map[string]any
	switch v.Type {
	case value.TypeMap:
		root = tomlTable(v)
	case value.TypeArray:
		root = map[string]any{"value": tomlValue(v)}
	default:
		root = map[string]any{"value": tomlValue(v)}
	}
	enc := toml.NewEncoder(w)
	if err := enc.Encode(sortTable(root)); err != nil {
		return fmt.Errorf("toml: %w", err)
	}
	return nil
}

// tomlTable превращает карту Value в map[string]any для toml.Encoder.
func tomlTable(v value.Value) map[string]any {
	out := make(map[string]any)
	for k, e := range v.MapVal() {
		if e.Type == value.TypeNil {
			continue // TOML не имеет null
		}
		out[k] = tomlValue(e)
	}
	return out
}

func tomlValue(v value.Value) any {
	switch v.Type {
	case value.TypeMap:
		return tomlTable(v)
	case value.TypeArray:
		a := v.ArrayVal()
		out := make([]any, len(a))
		for i, e := range a {
			out[i] = tomlValue(e)
		}
		return out
	default:
		return v.ToAny()
	}
}

// sortTable возвращает map с детерминированным порядком ключей.
// toml.Encoder сам сортирует ключи, но явная сортировка гарантирует
// стабильность вывода между версиями библиотеки.
func sortTable(m map[string]any) map[string]any {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out := make(map[string]any, len(m))
	for _, k := range keys {
		if sub, ok := m[k].(map[string]any); ok {
			out[k] = sortTable(sub)
		} else {
			out[k] = m[k]
		}
	}
	return out
}
