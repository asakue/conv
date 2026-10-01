package codec

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/asakue/conv/internal/value"
)

// INICodec реализует Codec для формата INI.
type INICodec struct{}

func (INICodec) Name() string         { return "ini" }
func (INICodec) Extensions() []string { return []string{".ini", ".cfg", ".conf"} }
func (INICodec) MIME() string         { return "text/plain" }

// Decode читает INI из r.
func (INICodec) Decode(r io.Reader) (value.Value, error) {
	result := make(map[string]any)
	global := make(map[string]string)
	current := ""

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			current = strings.TrimSpace(line[1 : len(line)-1])
			continue
		}
		key, val, ok := parseINIKeyValue(line)
		if !ok {
			continue
		}
		if current == "" {
			global[key] = val
		} else {
			if _, exists := result[current]; !exists {
				result[current] = make(map[string]string)
			}
			result[current].(map[string]string)[key] = val
		}
	}
	if err := scanner.Err(); err != nil {
		return value.Value{}, fmt.Errorf("ini: %w", err)
	}
	if len(global) > 0 {
		result["_"] = global
	}
	return toValue(result), nil
}

// Encode записывает значение в формате INI.
func (INICodec) Encode(w io.Writer, v value.Value) error {
	bw := bufio.NewWriter(w)
	defer bw.Flush()

	m := v.MapVal()
	if m == nil {
		return fmt.Errorf("ini: ожидалась карта")
	}
	return writeINIMap(bw, m)
}

func writeINIMap(bw *bufio.Writer, m map[string]value.Value) error {
	// Сначала глобальные значения (не-карты).
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	for _, k := range keys {
		v := m[k]
		if v.Type != value.TypeMap {
			if err := writeINIPair(bw, k, v); err != nil {
				return err
			}
		}
	}

	// Затем секции.
	for _, k := range keys {
		v := m[k]
		if v.Type == value.TypeMap {
			if _, err := fmt.Fprintf(bw, "\n[%s]\n", k); err != nil {
				return err
			}
			secKeys := make([]string, 0, len(v.MapVal()))
			for sk := range v.MapVal() {
				secKeys = append(secKeys, sk)
			}
			sort.Strings(secKeys)
			for _, sk := range secKeys {
				if err := writeINIPair(bw, sk, v.MapVal()[sk]); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func writeINIPair(bw *bufio.Writer, key string, v value.Value) error {
	val := iniScalarString(v)
	_, err := fmt.Fprintf(bw, "%s = %s\n", key, val)
	return err
}

// iniScalarString преобразует скалярное значение в строку.
func iniScalarString(v value.Value) string {
	switch v.Type {
	case value.TypeNil:
		return ""
	case value.TypeBool:
		if v.BoolVal() {
			return "true"
		}
		return "false"
	case value.TypeInt:
		return strconv.FormatInt(v.IntVal(), 10)
	case value.TypeFloat:
		return strconv.FormatFloat(v.FloatVal(), 'g', -1, 64)
	case value.TypeString:
		return v.StrVal()
	default:
		return v.ScalarString()
	}
}

func parseINIKeyValue(line string) (key, val string, ok bool) {
	// Поддержка key = value и key: value.
	for _, sep := range []string{"=", ":"} {
		if idx := strings.Index(line, sep); idx >= 0 {
			k := strings.TrimSpace(line[:idx])
			v := strings.TrimSpace(line[idx+len(sep):])
			// Убираем кавычки.
			v = strings.Trim(v, `"'`)
			return k, v, true
		}
	}
	return "", "", false
}
