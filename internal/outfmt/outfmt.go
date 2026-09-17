// Package outfmt emits human or structured CLI output.
package outfmt

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Emit writes v as text (fmt), json, or yaml based on format.
func Emit(w io.Writer, format string, human func(io.Writer) error, v any) error {
	if w == nil {
		w = os.Stdout
	}
	switch format {
	case "json":
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		return enc.Encode(v)
	case "yaml", "yml":
		b, err := yaml.Marshal(v)
		if err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	default:
		if human != nil {
			return human(w)
		}
		_, err := fmt.Fprintln(w, v)
		return err
	}
}
