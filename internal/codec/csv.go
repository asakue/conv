package codec

import (
	"bytes"
	"encoding/csv"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"

	"github.com/asakue/conv/internal/value"
)

// CSVCodec реализует формат CSV (RFC 4180) с поддержкой TSV.
//
// Декодирование: первая строка — заголовки, остальные — записи.
// Результат — массив карт (по одной на строку).
//
// Кодирование: принимает массив карт одинаковой схемы (или одну карту),
// пишет заголовки и строки. Вложенные структуры предварительно
// разворачиваются через value.Flatten (см. internal/app).
type CSVCodec struct {
	// Comma — разделитель полей ('\0' → автоопределение).
	Comma rune
	// Header — писать/читать строку заголовков.
	Header bool
	// Infer — восстанавливать типы из строк (int/float/bool/null).
	Infer bool
}

// NewCSV возвращает кодек CSV с запятой и заголовками.
func NewCSV() *CSVCodec { return &CSVCodec{Comma: ',', Header: true, Infer: true} }

// NewTSV возвращает кодек TSV.
func NewTSV() *CSVCodec { return &CSVCodec{Comma: '\t', Header: true, Infer: true} }

// Decode читает CSV/TSV из r.
func (c CSVCodec) Decode(r io.Reader) (value.Value, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return value.Value{}, fmt.Errorf("csv: %w", err)
	}
	return c.DecodeBytes(data)
}

// DecodeBytes разбирает байты CSV.
func (c CSVCodec) DecodeBytes(data []byte) (value.Value, error) {
	data = trimBOM(data)
	if len(strings.TrimSpace(string(data))) == 0 {
		return value.NewArray(nil), nil
	}
	cr := csv.NewReader(bytes.NewReader(data))
	if c.Comma != 0 {
		cr.Comma = c.Comma
	} else {
		cr.Comma = detectDelimiter(data)
	}
	cr.FieldsPerRecord = -1 // разрешаем разное число полей
	cr.LazyQuotes = true

	records, err := cr.ReadAll()
	if err != nil {
		return value.Value{}, fmt.Errorf("csv: %w", err)
	}
	if len(records) == 0 {
		return value.NewArray(nil), nil
	}

	var headers []string
	start := 0
	if c.Header {
		headers = records[0]
		start = 1
		// Генерируем имена для пустых заголовков.
		for i, h := range headers {
			if strings.TrimSpace(h) == "" {
				headers[i] = fmt.Sprintf("column%d", i+1)
			}
		}
	} else {
		headers = make([]string, len(records[0]))
		for i := range headers {
			headers[i] = fmt.Sprintf("column%d", i+1)
		}
	}

	rows := make([]value.Value, 0, len(records)-start)
	for _, rec := range records[start:] {
		// Пропускаем полностью пустые строки.
		if isBlankRecord(rec) {
			continue
		}
		m := make(map[string]value.Value, len(headers))
		for i, h := range headers {
			var v value.Value
			if i < len(rec) {
				v = csvCellToValue(rec[i], c.Infer)
			} else {
				v = value.NewNil()
			}
			m[h] = v
		}
		rows = append(rows, value.NewMap(m))
	}
	return value.NewArray(rows), nil
}

// Encode записывает данные в формате CSV.
func (c CSVCodec) Encode(w io.Writer, v value.Value) error {
	rows, headers := prepareCSVRows(v)
	cw := csv.NewWriter(w)
	if c.Comma != 0 {
		cw.Comma = c.Comma
	} else {
		cw.Comma = ','
	}
	if c.Header && len(headers) > 0 {
		if err := cw.Write(headers); err != nil {
			return fmt.Errorf("csv: %w", err)
		}
	}
	for _, row := range rows {
		rec := make([]string, len(headers))
		for i, h := range headers {
			if cell, ok := row[h]; ok {
				rec[i] = cell
			}
		}
		if err := cw.Write(rec); err != nil {
			return fmt.Errorf("csv: %w", err)
		}
	}
	cw.Flush()
	if err := cw.Error(); err != nil {
		return fmt.Errorf("csv: %w", err)
	}
	return nil
}

// prepareCSVRows превращает Value в список строк-карт (значение -> строка)
// и упорядоченный список заголовков.
func prepareCSVRows(v value.Value) ([]map[string]string, []string) {
	var items []value.Value
	switch v.Type {
	case value.TypeArray:
		items = v.ArrayVal()
	case value.TypeMap:
		items = []value.Value{v}
	case value.TypeNil:
		return nil, nil
	default:
		return []map[string]string{{"value": v.ScalarString()}}, []string{"value"}
	}

	// Разворачиваем каждую запись.
	opt := value.FlattenOptions{Sep: ".", ArrayStyle: "brackets"}
	rows := make([]map[string]string, 0, len(items))
	headerSet := map[string]struct{}{}
	for _, it := range items {
		var flat map[string]value.Value
		if it.Type == value.TypeMap {
			flat = value.Flatten(it, opt)
		} else {
			flat = map[string]value.Value{"value": it}
		}
		row := make(map[string]string, len(flat))
		for k, val := range flat {
			row[k] = csvCellFromValue(val)
			headerSet[k] = struct{}{}
		}
		rows = append(rows, row)
	}

	headers := make([]string, 0, len(headerSet))
	for h := range headerSet {
		headers = append(headers, h)
	}
	sort.Strings(headers)
	return rows, headers
}

// csvCellToValue превращает строку ячейки в Value.
func csvCellToValue(s string, infer bool) value.Value {
	if s == "" {
		return value.NewNil()
	}
	if !infer {
		return value.NewString(s)
	}
	return parseCSVCell(s)
}

// csvCellFromValue превращает Value в строку ячейки CSV.
// Скаляры пишутся как есть, массивы и карты — компактным JSON.
func csvCellFromValue(v value.Value) string {
	switch v.Type {
	case value.TypeNil:
		return ""
	case value.TypeArray, value.TypeMap:
		return value.Compact(v)
	default:
		return v.ScalarString()
	}
}

// parseCSVCell разбирает строку ячейки CSV, восстанавливая типы.
func parseCSVCell(s string) value.Value {
	switch strings.ToLower(s) {
	case "null", "nil", "~", "none", "empty", "":
		return value.NewNil()
	case "true", "yes", "on":
		return value.NewBool(true)
	case "false", "no", "off":
		return value.NewBool(false)
	}
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return value.NewInt(i)
	}
	if f, err := strconv.ParseFloat(s, 64); err == nil {
		return value.NewFloat(f)
	}
	return value.NewString(s)
}

// detectDelimiter определяет разделитель по частоте встречаемости
// в первой непустой строке.
func detectDelimiter(data []byte) rune {
	line, _, _ := bytes.Cut(data, []byte("\n"))
	line = bytes.TrimRight(line, "\r")
	candidates := []rune{',', ';', '\t', '|'}
	best := ','
	bestCount := -1
	for _, c := range candidates {
		n := bytes.Count(line, []byte(string(c)))
		if n > bestCount {
			bestCount = n
			best = c
		}
	}
	if bestCount <= 0 {
		return ','
	}
	return best
}

func isBlankRecord(rec []string) bool {
	for _, s := range rec {
		if strings.TrimSpace(s) != "" {
			return false
		}
	}
	return true
}

// trimBOM удаляет UTF-8 BOM из начала данных.
func trimBOM(data []byte) []byte {
	if len(data) >= 3 && data[0] == 0xEF && data[1] == 0xBB && data[2] == 0xBF {
		return data[3:]
	}
	return data
}

