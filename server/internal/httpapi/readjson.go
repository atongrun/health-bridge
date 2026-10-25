// Package httpapi exposes the shortcuts-1 upload endpoints. It authenticates
// before reading the body, bounds input size, and rejects malformed JSON
// (duplicate keys, non-finite numbers, excessive nesting) before business logic.
package httpapi

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strconv"
)

var (
	errDuplicateKey = errors.New("duplicate JSON key")
	errNonFinite    = errors.New("non-finite JSON number")
	errTooDeep      = errors.New("JSON nesting too deep")
)

type jframe struct {
	isObj        bool
	keys         map[string]struct{}
	expectingKey bool
}

// validateJSON walks the token stream once to reject duplicate object keys,
// non-finite numbers and nesting deeper than maxDepth. It is stricter than
// encoding/json's default decoder (which accepts NaN/Inf and last-wins keys).
func validateJSON(data []byte, maxDepth int) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	var stack []*jframe
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		switch t := tok.(type) {
		case json.Delim:
			switch t {
			case '{':
				stack = append(stack, &jframe{isObj: true, keys: map[string]struct{}{}, expectingKey: true})
				if len(stack) > maxDepth {
					return errTooDeep
				}
			case '[':
				stack = append(stack, &jframe{isObj: false})
				if len(stack) > maxDepth {
					return errTooDeep
				}
			case '}':
				stack = stack[:len(stack)-1]
				parentExpectsKey(stack)
			case ']':
				stack = stack[:len(stack)-1]
				parentExpectsKey(stack)
			}
		case string:
			if top := currentObj(stack); top != nil && top.expectingKey {
				if _, dup := top.keys[t]; dup {
					return fmt.Errorf("%w: %q", errDuplicateKey, t)
				}
				top.keys[t] = struct{}{}
				top.expectingKey = false
			} else if top != nil {
				top.expectingKey = true
			}
		case json.Number:
			if f, err := strconv.ParseFloat(string(t), 64); err == nil && (math.IsNaN(f) || math.IsInf(f, 0)) {
				return errNonFinite
			}
			if top := currentObj(stack); top != nil {
				top.expectingKey = true
			}
		default: // bool, nil
			if top := currentObj(stack); top != nil {
				top.expectingKey = true
			}
		}
	}
}

func currentObj(stack []*jframe) *jframe {
	if len(stack) == 0 {
		return nil
	}
	top := stack[len(stack)-1]
	if !top.isObj {
		return nil
	}
	return top
}

func parentExpectsKey(stack []*jframe) {
	if len(stack) == 0 {
		return
	}
	top := stack[len(stack)-1]
	if top.isObj {
		top.expectingKey = true
	}
}

// decodeStrict decodes into dst, rejecting unknown fields. Duplicate keys and
// non-finite numbers must already have been rejected by validateJSON.
func decodeStrict(data []byte, dst any) error {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected trailing JSON value")
	}
	return nil
}
