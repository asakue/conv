package codec

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/asakue/conv/internal/value"
)

// ENVCodec — декодер/энкодер для файлов .env / переменных окружения.
//
// Синтаксис:
//
//	K=VALUE          — простое значение
//	K="VALUE"        — значение в двойных кавычках (поддерживаются escape-последовательности)
//	K='VALUE'        — значение в одинарных кавычках (без обработки escape)
//	# комментарий    — игнорируется
//	экспорт K=VALUE  — префикс export (опционально)
//
// Декодирование возвращает одну карту (маппинг ключ → значение).
// Кодирование принимает карту или массив карт — ключи сортируются,
// значения, содержащие пробелы или спецсимволы, экранируются.
type ENVCodec struct{}

// NewENV возвращает кодек ENV.
func NewENV() *ENVCodec { return &ENVCodec{} }

// Decode читает .env-файл из r.
func (ENVCodec) Decode(r io.Reader) (value.Value, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 1024*1024), 10*1024*1024)
	m := make(map[string]value.Value)
	for sc.Scan() {
		raw := sc.Text()
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, val, ok := parseEnvLine(trimmed)
		if !ok {
			continue
		}
		m[key] = value.NewString(val)
	}
	if err := sc.Err(); err != nil {
		return value.Value{}, fmt.Errorf("env: %w", err)
	}
	if len(m) == 0 {
		return value.NewNil(), nil
	}
	return value.NewMap(m), nil
}

// parseEnvLine разбирает одну строку KEY=VALUE.
func parseEnvLine(line string) (string, string, bool) {
	// Убираем префикс export.
	if strings.HasPrefix(line, "export ") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export "))
	} else if strings.HasPrefix(line, "export\t") {
		line = strings.TrimSpace(strings.TrimPrefix(line, "export\t"))
	}

	// Ищем первый '='.
	idx := strings.Index(line, "=")
	if idx <= 0 {
		return "", "", false
	}
	key := strings.TrimSpace(line[:idx])
	if key == "" {
		return "", "", false
	}

	val, err := unquoteEnvValue(line[idx+1:])
	if err != nil {
		return "", "", false
	}
	return key, val, true
}

// unquoteEnvValue убирает кавычки и обрабатывает escape-последовательности.
func unquoteEnvValue(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", nil
	}
	if len(trimmed) >= 2 {
		if trimmed[0] == '"' {
			// Двойные кавычки — поддерживаем escape.
			var b strings.Builder
			i := 1
			for i < len(trimmed)-1 {
				ch := trimmed[i]
				if ch == '\\' {
					i++
					if i >= len(trimmed)-1 {
						return "", fmt.Errorf("незакрытый escape")
					}
					switch trimmed[i] {
					case '"':
						b.WriteByte('"')
					case '\\':
						b.WriteByte('\\')
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case 'r':
						b.WriteByte('\r')
					default:
						b.WriteByte('\\')
						b.WriteByte(trimmed[i])
					}
				} else {
					b.WriteByte(ch)
				}
				i++
			}
			return b.String(), nil
		}
		if trimmed[0] == '\'' {
			// Одинарные кавычки — без обработки escape.
			end := strings.Index(trimmed[1:], "'")
			if end < 0 {
				return "", fmt.Errorf("незакрытая одинарная кавычка")
			}
			return trimmed[1 : 1+end], nil
		}
	}
	// Без кавычек — возвращаем как есть, убирая trailing comment.
	return trimInlineComment(trimmed), nil
}

// trimInlineComment убирает inline-комментарии из некавыченных значений.
func trimInlineComment(s string) string {
	// Простая эвристика: # или // без кавычек.
	for i := 0; i < len(s); i++ {
		if s[i] == '#' || (i+1 < len(s) && s[i] == '/' && s[i+1] == '/') {
			return strings.TrimSpace(s[:i])
		}
	}
	return s
}

// Encode записывает данные в формате .env.
func (ENVCodec) Encode(w io.Writer, v value.Value) error {
	if v.Type == value.TypeArray {
		// Если массив — берём первую карту или объединяем.
		for _, item := range v.ArrayVal() {
			if item.Type == value.TypeMap {
				if err := encodeMap(w, item.MapVal()); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if v.Type != value.TypeMap {
		// Скаляр — записываем как value=...
		_, err := fmt.Fprintf(w, "value=%s\n", envQuote(v.ScalarString()))
		return err
	}
	return encodeMap(w, v.MapVal())
}

func encodeMap(w io.Writer, m map[string]value.Value) error {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		v := m[k]
		// Пропускаем null.
		if v.Type == value.TypeNil {
			continue
		}
		// Вложенные структуры сериализуем как JSON в кавычках.
		var s string
		switch v.Type {
		case value.TypeArray, value.TypeMap:
			s = value.Compact(v)
		default:
			s = v.ScalarString()
		}
		quoted := envQuote(s)
		if _, err := fmt.Fprintf(w, "%s=%s\n", k, quoted); err != nil {
			return err
		}
	}
	return nil
}

// envQuote экранирует значение для .env: если есть пробелы, спецсимволы или
// кавычки — оборачивает в двойные кавычки.
func envQuote(s string) string {
	// Проверяем, нужно ли экранировать.
	if needsQuoting(s) {
		// Экранируем обратные кавычки и двойные кавычки.
		var b strings.Builder
		b.WriteByte('"')
		for _, r := range s {
			switch r {
			case '"':
				b.WriteString(`\"`)
			case '\\':
				b.WriteString(`\\`)
			case '\n':
				b.WriteString(`\n`)
			case '\t':
				b.WriteString(`\t`)
			case '\r':
				b.WriteString(`\r`)
			default:
				b.WriteRune(r)
			}
		}
		b.WriteByte('"')
		return b.String()
	}
	return s
}

func needsQuoting(s string) bool {
	if s == "" {
		return true
	}
	for _, r := range s {
		switch r {
		case ' ', '\t', '\n', '\r', '"', '\'', '#', '=', '$', '`', '!', '&', '|', ';', '(', ')', '[', ']', '{', '}', '<', '>':
			return true
		}
	}
	return false
}