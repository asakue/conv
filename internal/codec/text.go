package codec

import (
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/asakue/conv/internal/value"
)

// TextCodec — простой текстовый кодек.
//
// Decode читает всё содержимое и возвращает строку.
// Encode записывает скаляр как есть, карту — как "key: value" пары.
type TextCodec struct{}

// Decode читает текст из r.
func (TextCodec) Decode(r io.Reader) (value.Value, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return value.Value{}, fmt.Errorf("text: %w", err)
	}
	return value.NewString(string(bytes.TrimSpace(data))), nil
}

// Encode записывает значение в текстовом формате.
func (TextCodec) Encode(w io.Writer, v value.Value) error {
	if v.Type == value.TypeMap {
		keys := make([]string, 0, len(v.MapVal()))
		for k := range v.MapVal() {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			val := v.MapVal()[k]
			if _, err := fmt.Fprintf(w, "%s: %s\n", k, val.ScalarString()); err != nil {
				return err
			}
		}
		return nil
	}
	_, err := fmt.Fprint(w, v.ScalarString())
	return err
}

// _ — unused import guard.
var _ = strings.Builder{}
