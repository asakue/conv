package codec

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strconv"

	"github.com/gurzi/conv/internal/value"
)

// JSONCodec реализует формат JSON (RFC 8259).
type JSONCodec struct {
	// Indent — строка отступа дляpretty-вывода. Пустая строка — компактный вывод.
	Indent string
}

// NewJSON возвращает энкодер с отступом в два пробела.
func NewJSON() *JSONCodec { return &JSONCodec{Indent: "  "} }

// Decode читает JSON из r.
func (JSONCodec) Decode(r io.Reader) (value.Value, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return value.Value{}, fmt.Errorf("json: %w", err)
	}
	return DecodeJSONBytes(data)
}

// DecodeJSONBytes разбирает байты JSON. Числа сохраняют целочисленность
// (через json.Number), чтобы 42 не превратилось в 42.0.
func DecodeJSONBytes(data []byte) (value.Value, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return value.NewNil(), nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	dec.UseNumber()
	var raw any
	if err := dec.Decode(&raw); err != nil {
		return value.Value{}, fmt.Errorf("json: %w", err)
	}
	v, err := fromStandardJSON(raw)
	if err != nil {
		return value.Value{}, fmt.Errorf("json: %w", err)
	}
	return v, nil
}

// fromStandardJSON конвертирует результат encoding/json в Value.
func fromStandardJSON(raw any) (value.Value, error) {
	switch t := raw.(type) {
	case nil:
		return value.NewNil(), nil
	case bool:
		return value.NewBool(t), nil
	case string:
		return value.NewString(t), nil
	case json.Number:
		return numberToValue(string(t))
	case []any:
		out := make([]value.Value, len(t))
		for i, e := range t {
			ev, err := fromStandardJSON(e)
			if err != nil {
				return value.Value{}, err
			}
			out[i] = ev
		}
		return value.NewArray(out), nil
	case map[string]any:
		out := make(map[string]value.Value, len(t))
		for k, e := range t {
			ev, err := fromStandardJSON(e)
			if err != nil {
				return value.Value{}, err
			}
			out[k] = ev
		}
		return value.NewMap(out), nil
	default:
		return value.Value{}, fmt.Errorf("неподдерживаемый тип %T", raw)
	}
}

// numberToValue разбирает json.Number как int64 либо float64.
func numberToValue(s string) (value.Value, error) {
	if i, err := strconv.ParseInt(s, 10, 64); err == nil {
		return value.NewInt(i), nil
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return value.Value{}, fmt.Errorf("не число %q: %w", s, err)
	}
	return value.NewFloat(f), nil
}

// Encode записывает значение в формате JSON.
func (c JSONCodec) Encode(w io.Writer, v value.Value) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	if c.Indent != "" {
		enc.SetIndent("", c.Indent)
	}
	if err := enc.Encode(jsonValue(v)); err != nil {
		return fmt.Errorf("json: %w", err)
	}
	return nil
}

// jsonValue подготавливает данные для encoding/json:
// сохраняет целые числа как int64, float без дробной части пишет как int,
// чтобы 42.0 не превращалось в 42.0 (JSON не различает типы).
func jsonValue(v value.Value) any {
	switch v.Type {
	case value.TypeMap:
		m := v.MapVal()
		out := make(map[string]any, len(m))
		for k, e := range m {
			out[k] = jsonValue(e)
		}
		return out
	case value.TypeArray:
		a := v.ArrayVal()
		out := make([]any, len(a))
		for i, e := range a {
			out[i] = jsonValue(e)
		}
		return out
	case value.TypeFloat:
		f, _ := v.Value.(float64)
		if math.IsInf(f, 0) || math.IsNaN(f) {
			// JSON не поддерживает inf/nan — кодируем строкой.
			return strconv.FormatFloat(f, 'g', -1, 64)
		}
		return f
	default:
		return v.ToAny()
	}
}

// NDJSONCodec реализует формат NDJSON (он же JSON Lines).
//
// При декодировании каждая непустая строка парсится как отдельное
// JSON-значение, результатом становится массив. При кодировании
// верхнеуровневый массив пишется по одному объекту в строке;
// одиночный объект/скаляр пишется одной строкой.
type NDJSONCodec struct{}

// NewNDJSON возвращает кодек NDJSON.
func NewNDJSON() *NDJSONCodec { return &NDJSONCodec{} }

// Decode читает NDJSON, пропуская пустые строки.
func (NDJSONCodec) Decode(r io.Reader) (value.Value, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 32*1024*1024)
	var out []value.Value
	line := 0
	for sc.Scan() {
		line++
		b := bytes.TrimSpace(sc.Bytes())
		if len(b) == 0 {
			continue
		}
		v, err := DecodeJSONBytes(b)
		if err != nil {
			return value.Value{}, fmt.Errorf("ndjson, строка %d: %w", line, err)
		}
		out = append(out, v)
	}
	if err := sc.Err(); err != nil {
		return value.Value{}, fmt.Errorf("ndjson: %w", err)
	}
	return value.NewArray(out), nil
}

// Encode записывает NDJSON.
func (NDJSONCodec) Encode(w io.Writer, v value.Value) error {
	items := []value.Value{v}
	if v.Type == value.TypeArray {
		items = v.ArrayVal()
	}
	bw := bufio.NewWriter(w)
	for _, e := range items {
		data, err := json.Marshal(jsonValue(e))
		if err != nil {
			return fmt.Errorf("ndjson: %w", err)
		}
		if _, err := bw.Write(data); err != nil {
			return fmt.Errorf("ndjson: %w", err)
		}
		if err := bw.WriteByte('\n'); err != nil {
			return fmt.Errorf("ndjson: %w", err)
		}
	}
	return bw.Flush()
}