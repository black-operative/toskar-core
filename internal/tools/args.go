package tools

import (
	"encoding/json"
	"errors"
	"fmt"

	"github.com/yeixio/toskar-core/internal/structured"
)

// ErrInvalidArgs is returned for a call whose arguments have the wrong type.
var ErrInvalidArgs = errors.New("invalid arguments")

// argTypes are the types a tool's Schema may name. Any other value, such as
// "owner/name", describes a string.
var argTypes = map[string]bool{"string": true, "integer": true, "number": true, "boolean": true, "object": true, "array": true}

// argSchema turns a tool's Schema, such as {"repo":"owner/name","number":"integer"},
// into a schema. Arguments are not required here; the tool says what it needs.
func argSchema(def Definition) *structured.Schema {
	var fields map[string]any
	if err := json.Unmarshal([]byte(def.Schema), &fields); err != nil || len(fields) == 0 {
		return nil
	}
	s := &structured.Schema{Type: "object", Properties: map[string]*structured.Schema{}}
	for name, v := range fields {
		typ, _ := v.(string)
		if !argTypes[typ] {
			typ = "string"
		}
		s.Properties[name] = &structured.Schema{Type: typ}
	}
	return s
}

// CheckArgs repairs a call's arguments where it is safe, such as "7" for a
// whole number, and refuses ones of the wrong type before the tool runs
// (spec §27), naming the problem so the model can call again.
func CheckArgs(def Definition, args map[string]any) (map[string]any, error) {
	s := argSchema(def)
	if s == nil || args == nil {
		return args, nil
	}
	fixed, _ := structured.Coerce(args, s).(map[string]any)
	if issues := structured.Validate(fixed, s); len(issues) > 0 {
		return args, fmt.Errorf("%w: %s", ErrInvalidArgs, issues[0].String())
	}
	return fixed, nil
}
