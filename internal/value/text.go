package value

import (
	"bytes"
	"encoding/json"
)

// Compact возвращает JSON-подобное представление значения в одну строку.
//
// Используется форматами без типизации (CSV, ENV, INI) для вложенных
// структур, а также для отладочного вывода.
func Compact(v Value) string {
	return encode(v, "", false)
}

// Pretty возвращает отформатированное JSON-подобное представление.
func Pretty(v Value, indent string) string {
	return encode(v, indent, true)
}

func encode(v Value, indent string, pretty bool) string {
	buf := &bytes.Buffer{}
	enc := json.NewEncoder(buf)
	enc.SetEscapeHTML(false)
	if pretty {
		enc.SetIndent("", indent)
	}
	// ToAny никогда не возвращает ошибку маршалинга для наших типов,
	// кроме неизменяемых карт — они все копируются.
	if err := enc.Encode(v.ToAny()); err != nil {
		return `"` + escapeSpecials(v.String()) + `"`
	}
	return trimTrailingNewline(buf.String())
}

func trimTrailingNewline(s string) string {
	for len(s) > 0 && (s[len(s)-1] == '\n' || s[len(s)-1] == '\r') {
		s = s[:len(s)-1]
	}
	return s
}

func escapeSpecials(s string) string {
	b, err := json.Marshal(s)
	if err != nil {
		return s
	}
	return string(b)
}

// String возвращает человекочитаемое имя значения для логов и ошибок.
func (v Value) String() string {
	switch v.Type {
	case TypeNil:
		return "null"
	case TypeString, TypeInt, TypeFloat, TypeBool, TypeTime:
		return v.ScalarString()
	case TypeArray:
		return "[array:" + itoa(len(v.ArrayVal())) + "]"
	case TypeMap:
		return "[map:" + itoa(len(v.MapVal())) + "]"
	default:
		return "<unknown>"
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}