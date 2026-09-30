// Package value предоставляет единую модель данных для всех форматов.
//
// Все декодеры возвращают Value, все энкодеры принимают Value.
// Это позволяет конвертировать между любыми форматами без промежуточных
// структур и потерь типизации там, где формат их поддерживает.
package value

import (
	"sort"
	"strconv"
	"strings"
	"time"
)

// Type перечисляет возможные типы значений.
type Type int

const (
	TypeNil Type = iota
	TypeString
	TypeInt
	TypeFloat
	TypeBool
	TypeTime
	TypeArray
	TypeMap
)

func (t Type) String() string {
	switch t {
	case TypeNil:
		return "null"
	case TypeString:
		return "string"
	case TypeInt:
		return "int"
	case TypeFloat:
		return "float"
	case TypeBool:
		return "bool"
	case TypeTime:
		return "time"
	case TypeArray:
		return "array"
	case TypeMap:
		return "map"
	default:
		return "unknown"
	}
}

// Value — единое представление данных любого формата.
//
// Поле Value содержит Go-значение, соответствующее Type:
//
//	TypeNil   -> nil
//	TypeString-> string
//	TypeInt   -> int64
//	TypeFloat -> float64
//	TypeBool  -> bool
//	TypeTime  -> time.Time
//	TypeArray -> []Value
//	TypeMap   -> map[string]Value
type Value struct {
	Type  Type
	Value any
}

// IsNull сообщает, является ли значение null.
func (v Value) IsNull() bool { return v.Type == TypeNil }

// MapVal возвращает содержимое для TypeMap (nil для остальных типов).
func (v Value) MapVal() map[string]Value {
	if v.Type != TypeMap {
		return nil
	}
	m, _ := v.Value.(map[string]Value)
	return m
}

// ArrayVal возвращает содержимое для TypeArray (nil для остальных типов).
func (v Value) ArrayVal() []Value {
	if v.Type != TypeArray {
		return nil
	}
	a, _ := v.Value.([]Value)
	return a
}

// IsScalar сообщает, что значение — скаляр (не массив и не карта).
func (v Value) IsScalar() bool {
	switch v.Type {
	case TypeArray, TypeMap:
		return false
	default:
		return true
	}
}

// Keys возвращает отсортированные ключи карты (пусто для не-карт).
func (v Value) Keys() []string {
	m := v.MapVal()
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Constructors ---------------------------------------------------------------

func NewNil() Value                  { return Value{Type: TypeNil} }
func NewString(s string) Value       { return Value{Type: TypeString, Value: s} }
func NewInt(i int64) Value           { return Value{Type: TypeInt, Value: i} }
func NewFloat(f float64) Value       { return Value{Type: TypeFloat, Value: f} }
func NewBool(b bool) Value           { return Value{Type: TypeBool, Value: b} }
func NewTime(t time.Time) Value      { return Value{Type: TypeTime, Value: t} }
func NewMap(m map[string]Value) Value { return Value{Type: TypeMap, Value: m} }
func NewArray(a []Value) Value       { return Value{Type: TypeArray, Value: a} }

// NewMapFromAny строит карту из map[string]any (удобно для тестов и JSON).
func NewMapFromAny(m map[string]any) Value {
	out := make(map[string]Value, len(m))
	for k, v := range m {
		out[k] = FromAny(v)
	}
	return NewMap(out)
}

// FromAny приводит нативное Go-значение к единой модели Value.
//
// Поддерживаются скаляры, map[string]any, map[string]string, []any и
// типизированные срезы. Неизвестные типы превращаются в строку через
// fmt.Sprintf("%v").
func FromAny(v any) Value {
	switch val := v.(type) {
	case nil:
		return NewNil()
	case Value:
		return val
	case string:
		return NewString(val)
	case bool:
		return NewBool(val)
	case int:
		return NewInt(int64(val))
	case int8:
		return NewInt(int64(val))
	case int16:
		return NewInt(int64(val))
	case int32:
		return NewInt(int64(val))
	case int64:
		return NewInt(val)
	case uint:
		return NewInt(int64(val))
	case uint8:
		return NewInt(int64(val))
	case uint16:
		return NewInt(int64(val))
	case uint32:
		return NewInt(int64(val))
	case uint64:
		return NewInt(int64(val))
	case float32:
		return NewFloat(float64(val))
	case float64:
		return NewFloat(val)
	case time.Time:
		return NewTime(val)
	case map[string]Value:
		return NewMap(val)
	case map[string]any:
		return NewMapFromAny(val)
	case map[string]string:
		out := make(map[string]Value, len(val))
		for k, s := range val {
			out[k] = NewString(s)
		}
		return NewMap(out)
	case []Value:
		return NewArray(val)
	case []any:
		out := make([]Value, len(val))
		for i, e := range val {
			out[i] = FromAny(e)
		}
		return NewArray(out)
	case []string:
		out := make([]Value, len(val))
		for i, s := range val {
			out[i] = NewString(s)
		}
		return NewArray(out)
	case []int:
		out := make([]Value, len(val))
		for i, n := range val {
			out[i] = NewInt(int64(n))
		}
		return NewArray(out)
	case []float64:
		out := make([]Value, len(val))
		for i, f := range val {
			out[i] = NewFloat(f)
		}
		return NewArray(out)
	case []bool:
		out := make([]Value, len(val))
		for i, b := range val {
			out[i] = NewBool(b)
		}
		return NewArray(out)
	default:
		return NewString(anyToString(val))
	}
}

// ToAny разворачивает Value в нативные Go-значения: map[string]any и []any.
// Такой вид удобно передавать в encoding/json.
func (v Value) ToAny() any {
	switch v.Type {
	case TypeNil:
		return nil
	case TypeMap:
		m := v.MapVal()
		out := make(map[string]any, len(m))
		for k, e := range m {
			out[k] = e.ToAny()
		}
		return out
	case TypeArray:
		a := v.ArrayVal()
		out := make([]any, len(a))
		for i, e := range a {
			out[i] = e.ToAny()
		}
		return out
	case TypeTime:
		t, _ := v.Value.(time.Time)
		return t.Format(time.RFC3339Nano)
	default:
		return v.Value
	}
}

// ScalarString возвращает строковое представление скаляра.
// Для массивов и карт возвращает JSON-подобное представление.
func (v Value) ScalarString() string {
	switch v.Type {
	case TypeNil:
		return ""
	case TypeString:
		s, _ := v.Value.(string)
		return s
	case TypeInt:
		n, _ := v.Value.(int64)
		return strconv.FormatInt(n, 10)
	case TypeFloat:
		f, _ := v.Value.(float64)
		return strconv.FormatFloat(f, 'f', -1, 64)
	case TypeBool:
		b, _ := v.Value.(bool)
		return strconv.FormatBool(b)
	case TypeTime:
		t, _ := v.Value.(time.Time)
		return t.Format(time.RFC3339Nano)
	default:
		return Compact(v)
	}
}

// ParseScalar пытается разобрать строку как скаляр: bool, int, float, null.
// Используется форматами без типизации (CSV, ENV, INI).
func ParseScalar(s string) Value {
	t := strings.TrimSpace(s)
	if t == "" {
		return NewString(s)
	}
	switch strings.ToLower(t) {
	case "null", "nil", "~":
		return NewNil()
	case "true", "yes", "on":
		return NewBool(true)
	case "false", "no", "off":
		return NewBool(false)
	}
	if i, err := strconv.ParseInt(t, 10, 64); err == nil {
		return NewInt(i)
	}
	if f, err := strconv.ParseFloat(t, 64); err == nil {
		return NewFloat(f)
	}
	if ts, ok := parseTimeString(t); ok {
		return NewTime(ts)
	}
	return NewString(s)
}

// timeLayouts — форматы, которые ParseScalar пробует для строки со временем.
var timeLayouts = []string{
	time.RFC3339Nano,
	time.RFC3339,
	"2006-01-02T15:04:05",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

func parseTimeString(s string) (time.Time, bool) {
	// Требует наличия разделителя даты, чтобы "2024" не стал датой.
	if !strings.Contains(s, "-") && !strings.Contains(s, ":") {
		return time.Time{}, false
	}
	for _, layout := range timeLayouts {
		if ts, err := time.Parse(layout, s); err == nil {
			return ts, true
		}
	}
	return time.Time{}, false
}

// Equal сравнивает два значения структурно.
func Equal(a, b Value) bool {
	if a.Type != b.Type {
		return false
	}
	switch a.Type {
	case TypeNil:
		return true
	case TypeMap:
		am, bm := a.MapVal(), b.MapVal()
		if len(am) != len(bm) {
			return false
		}
		for k, av := range am {
			bv, ok := bm[k]
			if !ok || !Equal(av, bv) {
				return false
			}
		}
		return true
	case TypeArray:
		aa, ba := a.ArrayVal(), b.ArrayVal()
		if len(aa) != len(ba) {
			return false
		}
		for i := range aa {
			if !Equal(aa[i], ba[i]) {
				return false
			}
		}
		return true
	default:
		return a.Value == b.Value
	}
}

// DeepEqual — псевдоним Equal для читаемости вызовов.
func DeepEqual(a, b Value) bool { return Equal(a, b) }

// Clone возвращает глубокую копию значения.
func Clone(v Value) Value {
	switch v.Type {
	case TypeMap:
		m := v.MapVal()
		out := make(map[string]Value, len(m))
		for k, e := range m {
			out[k] = Clone(e)
		}
		return NewMap(out)
	case TypeArray:
		a := v.ArrayVal()
		out := make([]Value, len(a))
		for i, e := range a {
			out[i] = Clone(e)
		}
		return NewArray(out)
	default:
		return v
	}
}
