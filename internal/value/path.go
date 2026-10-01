package value

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Путь — система адресации внутри дерева значений.
//
// Синтаксис: a.b.c для карт и a.items[2].name для массивов.
// Сегменты карт могут содержать точки, если путь задан в квадратных
// скобках: a["key.with.dots"].b

// SplitPath разбирает путь в список сегментов.
// Числовые сегменты интерпретируются как индексы массивов на этапе Get/Set.
func SplitPath(path string) ([]string, error) {
	if strings.TrimSpace(path) == "" {
		return nil, nil
	}
	var segs []string
	var cur strings.Builder
	inBracket := false
	inQuote := byte(0)
	escaped := false

	flush := func() {
		s := cur.String()
		cur.Reset()
		if s != "" {
			segs = append(segs, s)
		}
	}

	for i := 0; i < len(path); i++ {
		ch := path[i]
		if escaped {
			cur.WriteByte(ch)
			escaped = false
			continue
		}
		switch {
		case ch == '\\':
			escaped = true
		case inQuote != 0:
			if ch == inQuote {
				inQuote = 0
			} else {
				cur.WriteByte(ch)
			}
		case ch == '"' || ch == '\'':
			inQuote = ch
		case ch == '[':
			inBracket = true
			flush()
		case ch == ']':
			if !inBracket {
				return nil, fmt.Errorf("лишняя ']' в пути %q", path)
			}
			inBracket = false
			flush()
		case ch == '.' && !inBracket:
			flush()
		default:
			cur.WriteByte(ch)
		}
	}
	if inBracket || inQuote != 0 {
		return nil, fmt.Errorf("незакрытая скобка или кавычка в пути %q", path)
	}
	flush()
	return segs, nil
}

// Get извлекает значение по пути. Отсутствие пути (пустой путь) — корень.
func Get(root Value, path string) (Value, error) {
	segs, err := SplitPath(path)
	if err != nil {
		return Value{}, err
	}
	cur := root
	for _, seg := range segs {
		switch cur.Type {
		case TypeMap:
			m := cur.MapVal()
			v, ok := m[seg]
			if !ok {
				return Value{}, fmt.Errorf("ключ %q не найден", seg)
			}
			cur = v
		case TypeArray:
			idx, err := strconv.Atoi(seg)
			if err != nil {
				return Value{}, fmt.Errorf("сегмент %q не является индексом массива", seg)
			}
			a := cur.ArrayVal()
			if idx < 0 || idx >= len(a) {
				return Value{}, fmt.Errorf("индекс %d вне диапазона [0, %d)", idx, len(a))
			}
			cur = a[idx]
		default:
			return Value{}, fmt.Errorf("нельзя войти в скаляр типа %s по сегменту %q", cur.Type, seg)
		}
	}
	return cur, nil
}

// Set записывает значение по пути, создавая промежуточные карты.
// Промежуточные числовые сегменты создают массивы.
func Set(root Value, path string, val Value) (Value, error) {
	segs, err := SplitPath(path)
	if err != nil {
		return root, err
	}
	if len(segs) == 0 {
		return val, nil
	}
	return setIn(root, segs, val)
}

func setIn(cur Value, segs []string, val Value) (Value, error) {
	seg := segs[0]
	if len(segs) == 1 {
		switch cur.Type {
		case TypeMap:
			m := cloneMap(cur.MapVal())
			m[seg] = val
			return NewMap(m), nil
		case TypeArray:
			idx, err := strconv.Atoi(seg)
			if err != nil {
				return Value{}, fmt.Errorf("сегмент %q не является индексом массива", seg)
			}
			a := append([]Value{}, cur.ArrayVal()...)
			for len(a) <= idx {
				a = append(a, NewNil())
			}
			a[idx] = val
			return NewArray(a), nil
		case TypeNil:
			if isNumeric(seg) {
				return setIn(NewArray(nil), segs, val)
			}
			return setIn(NewMap(map[string]Value{}), segs, val)
		default:
			return Value{}, fmt.Errorf("нельзя записать в скаляр типа %s", cur.Type)
		}
	}

	// Есть вложенные сегменты — спускаемся.
	switch cur.Type {
	case TypeMap:
		m := cloneMap(cur.MapVal())
		child, ok := m[seg]
		if !ok {
			child = newValueForSegment(segs[1])
		}
		updated, err := setIn(child, segs[1:], val)
		if err != nil {
			return Value{}, err
		}
		m[seg] = updated
		return NewMap(m), nil
	case TypeArray:
		idx, err := strconv.Atoi(seg)
		if err != nil {
			return Value{}, fmt.Errorf("сегмент %q не является индексом массива", seg)
		}
		a := append([]Value{}, cur.ArrayVal()...)
		for len(a) <= idx {
			a = append(a, newValueForSegment(segs[1]))
		}
		updated, err := setIn(a[idx], segs[1:], val)
		if err != nil {
			return Value{}, err
		}
		a[idx] = updated
		return NewArray(a), nil
	case TypeNil:
		return setIn(newValueForSegment(seg), segs, val)
	default:
		return Value{}, fmt.Errorf("нельзя спуститься в скаляр типа %s", cur.Type)
	}
}

func newValueForSegment(seg string) Value {
	if isNumeric(seg) {
		return NewArray(nil)
	}
	return NewMap(map[string]Value{})
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	_, err := strconv.Atoi(s)
	return err == nil
}

func cloneMap(m map[string]Value) map[string]Value {
	out := make(map[string]Value, len(m))
	for k, v := range m {
		out[k] = v
	}
	return out
}

// Delete удаляет значение по пути.
func Delete(root Value, path string) (Value, error) {
	segs, err := SplitPath(path)
	if err != nil {
		return root, err
	}
	if len(segs) == 0 {
		return NewNil(), nil
	}
	return deleteIn(root, segs)
}

func deleteIn(cur Value, segs []string) (Value, error) {
	seg := segs[0]
	if len(segs) == 1 {
		switch cur.Type {
		case TypeMap:
			m := cloneMap(cur.MapVal())
			if _, ok := m[seg]; !ok {
				return Value{}, fmt.Errorf("ключ %q не найден", seg)
			}
			delete(m, seg)
			return NewMap(m), nil
		case TypeArray:
			idx, err := strconv.Atoi(seg)
			if err != nil {
				return Value{}, fmt.Errorf("сегмент %q не является индексом массива", seg)
			}
			a := cur.ArrayVal()
			if idx < 0 || idx >= len(a) {
				return Value{}, fmt.Errorf("индекс %d вне диапазона", idx)
			}
			out := append([]Value{}, a[:idx]...)
			out = append(out, a[idx+1:]...)
			return NewArray(out), nil
		default:
			return Value{}, fmt.Errorf("удалять не из чего: тип %s", cur.Type)
		}
	}
	switch cur.Type {
	case TypeMap:
		m := cloneMap(cur.MapVal())
		child, ok := m[seg]
		if !ok {
			return Value{}, fmt.Errorf("ключ %q не найден", seg)
		}
		updated, err := deleteIn(child, segs[1:])
		if err != nil {
			return Value{}, err
		}
		m[seg] = updated
		return NewMap(m), nil
	case TypeArray:
		idx, err := strconv.Atoi(seg)
		if err != nil {
			return Value{}, fmt.Errorf("сегмент %q не является индексом массива", seg)
		}
		a := cur.ArrayVal()
		if idx < 0 || idx >= len(a) {
			return Value{}, fmt.Errorf("индекс %d вне диапазона", idx)
		}
		out := append([]Value{}, a...)
		updated, err := deleteIn(a[idx], segs[1:])
		if err != nil {
			return Value{}, err
		}
		out[idx] = updated
		return NewArray(out), nil
	default:
		return Value{}, fmt.Errorf("нельзя спуститься в скаляр типа %s", cur.Type)
	}
}

// ---------------------------------------------------------------------------
// Flattening — разворачивание дерева в плоскую карту "путь -> скаляр".
// Используется CSV, ENV и INI, а также командой flatten.
// ---------------------------------------------------------------------------

// FlattenOptions управляет разворачиванием.
type FlattenOptions struct {
	Sep         string // разделитель путей, по умолчанию "."
	ArrayStyle  string // "brackets" (a[0]) или "dot" (a.0)
	NullAsEmpty bool   // null превращать в пустую строку
	MaxDepth    int    // 0 — без ограничений
}

// DefaultFlattenOptions возвращает разумные настройки по умолчанию.
func DefaultFlattenOptions() FlattenOptions {
	return FlattenOptions{Sep: ".", ArrayStyle: "brackets"}
}

// Flatten разворачивает дерево в плоскую карту путь -> значение.
// Массивы скаляров сохраняются целиком (в CSV/ENV их сериализуют энкодеры),
// если MergeScalarArrays == false; см. FlattenWith.
func Flatten(v Value, opt FlattenOptions) map[string]Value {
	return flattenWith(v, opt, false)
}

// FlattenLeafArrays разворачивает дерево, превращая каждый элемент массива
// в отдельный ключ (нужно для round-trip CSV/ENV).
func FlattenLeafArrays(v Value, opt FlattenOptions) map[string]Value {
	return flattenWith(v, opt, true)
}

func flattenWith(v Value, opt FlattenOptions, splitArrays bool) map[string]Value {
	if opt.Sep == "" {
		opt.Sep = "."
	}
	out := map[string]Value{}
	flattenWalk(v, "", opt, splitArrays, 0, out)
	return out
}

func flattenWalk(v Value, prefix string, opt FlattenOptions, splitArrays bool, depth int, out map[string]Value) {
	if opt.MaxDepth > 0 && depth >= opt.MaxDepth {
		out[prefix] = v
		return
	}
	switch v.Type {
	case TypeMap:
		m := v.MapVal()
		if len(m) == 0 && prefix != "" {
			out[prefix] = v
			return
		}
		for _, k := range sortedKeys(m) {
			child := prefix
			if child == "" {
				child = escapeSegment(k, opt.Sep)
			} else {
				child = child + opt.Sep + escapeSegment(k, opt.Sep)
			}
			flattenWalk(m[k], child, opt, splitArrays, depth+1, out)
		}
	case TypeArray:
		a := v.ArrayVal()
		if !splitArrays && isScalarArray(a) {
			out[prefix] = v
			return
		}
		if len(a) == 0 {
			out[prefix] = v
			return
		}
		for i, e := range a {
			var child string
			switch opt.ArrayStyle {
			case "dot":
				child = prefix + opt.Sep + strconv.Itoa(i)
			default:
				child = prefix + "[" + strconv.Itoa(i) + "]"
			}
			flattenWalk(e, child, opt, splitArrays, depth+1, out)
		}
	default:
		if v.Type == TypeNil && opt.NullAsEmpty {
			out[prefix] = NewString("")
			return
		}
		out[prefix] = v
	}
}

func isScalarArray(a []Value) bool {
	for _, e := range a {
		if !e.IsScalar() {
			return false
		}
	}
	return true
}

func escapeSegment(k, sep string) string {
	if strings.ContainsAny(k, sep+"[]") {
		return "[" + strings.ReplaceAll(k, "[", "\\[") + "]"
	}
	return k
}

func sortedKeys(m map[string]Value) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// Unflatten собирает дерево из плоской карты. Ключи вида a.b[0].c
// разбираются через SplitPath.
func Unflatten(flat map[string]Value, sep string) Value {
	if sep == "" {
		sep = "."
	}
	root := NewMap(map[string]Value{})
	keys := make([]string, 0, len(flat))
	for k := range flat {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		segs, err := splitFlatKey(k, sep)
		if err != nil {
			segs = []string{k}
		}
		updated, err := setIn(root, segs, flat[k])
		if err == nil {
			root = updated
		}
	}
	return root
}

// splitFlatKey разбирает ключ flatten в сегменты (a.b[2].c -> [a b 2 c]).
func splitFlatKey(key, sep string) ([]string, error) {
	if !strings.Contains(key, sep) && !strings.ContainsAny(key, "[") {
		return []string{key}, nil
	}
	var segs []string
	var cur strings.Builder
	inBracket := false
	for i := 0; i < len(key); i++ {
		ch := key[i]
		switch {
		case ch == '\\' && i+1 < len(key):
			i++
			cur.WriteByte(key[i])
		case ch == '[' && !inBracket:
			inBracket = true
			if cur.Len() > 0 {
				segs = append(segs, cur.String())
				cur.Reset()
			}
		case ch == ']' && inBracket:
			inBracket = false
			if cur.Len() > 0 {
				segs = append(segs, cur.String())
				cur.Reset()
			}
		case ch == '.' && !inBracket:
			if cur.Len() > 0 {
				segs = append(segs, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(ch)
		}
	}
	if cur.Len() > 0 {
		segs = append(segs, cur.String())
	}
	if len(segs) == 0 {
		return nil, fmt.Errorf("пустой ключ")
	}
	return segs, nil
}

// MergeDeep рекурсивно объединяет overlay в base (для -m/--merge).
// Скаляры из overlay перезаписывают base, карты сливаются, массивы
// перезаписываются целиком (если appendArrays == true — конкатенируются).
func Merge(base, overlay Value, appendArrays bool) Value {
	switch {
	case base.Type == TypeNil:
		return Clone(overlay)
	case overlay.Type == TypeNil:
		return Clone(base)
	case base.Type == TypeMap && overlay.Type == TypeMap:
		out := cloneMap(base.MapVal())
		for k, ov := range overlay.MapVal() {
			if bv, ok := out[k]; ok {
				out[k] = Merge(bv, ov, appendArrays)
			} else {
				out[k] = Clone(ov)
			}
		}
		return NewMap(out)
	case base.Type == TypeArray && overlay.Type == TypeArray && appendArrays:
		a := append([]Value{}, base.ArrayVal()...)
		for _, e := range overlay.ArrayVal() {
			a = append(a, Clone(e))
		}
		return NewArray(a)
	default:
		return Clone(overlay)
	}
}