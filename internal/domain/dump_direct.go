// dump_direct.go — FIX17 (perf, 2026-10-06): decoding projection rows into
// domain row types WITHOUT the JSON round trip, giving exactly what
// json.Unmarshal of the canonical dump gives.
//
// The dump (store.DumpProjectionWhere) encodes each row as a JSON object of
// column → value, where a value is only ever a string, an int64, a float64
// or nil (store.dumpRows), and dumpSection decodes that array into []T. At
// season scale that encode/decode was most of the cost of every whole-table
// read (~100 ms for the trip table at Ice Road, most pages several times).
//
// decodeRowsDirect replays json.Unmarshal's rules for the narrow shapes the
// domain row types actually use, and DECLINES (ok=false → the caller runs
// the JSON path) whenever it cannot be certain:
//
//   - T must be a struct with no embedded fields and no `,string` option;
//     its fields are matched to columns the way encoding/json matches object
//     keys — the json tag name (or the field name), exact match, `-` skips —
//     and a column that matches no field exactly but some field only
//     case-insensitively, or a name two fields share, declines;
//   - a matched field must be string, int, int64, float64, or a pointer to
//     string or int64;
//   - per value: nil leaves the field as it is (JSON null is a no-op on a
//     non-pointer and nil on a pointer); a string goes only into a string
//     (with json.Marshal's replacement of invalid UTF-8, byte for byte); an
//     int64 into an integer or a float64 field; a float64 only into a
//     float64. Any other pairing — every one JSON would reject or convert
//     through text — declines the whole read.
//
// The decision is per call, so a read either decodes wholly directly or
// wholly through JSON, never a mixture. dump_direct_test.go checks the two
// paths against each other on every row type the domain reads with.
package domain

import (
	"reflect"
	"strings"
	"sync"
	"unicode/utf8"
)

type directField struct {
	index []int
	kind  directKind
}

type directKind int

const (
	dkSkip directKind = iota
	dkString
	dkInt
	dkFloat
	dkPtrString
	dkPtrInt64
)

type directPlan struct {
	ok     bool
	fields []directField // per column
}

var directPlans sync.Map // directPlanKey -> *directPlan

type directPlanKey struct {
	t    reflect.Type
	cols string
}

func directPlanFor(t reflect.Type, cols []string) *directPlan {
	key := directPlanKey{t, strings.Join(cols, "\x00")}
	if p, ok := directPlans.Load(key); ok {
		return p.(*directPlan)
	}
	p := buildDirectPlan(t, cols)
	directPlans.Store(key, p)
	return p
}

func buildDirectPlan(t reflect.Type, cols []string) *directPlan {
	no := &directPlan{}
	if t.Kind() != reflect.Struct {
		return no
	}
	byName := map[string][]int{} // json name -> field index
	var names []string
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		if f.Anonymous {
			return no
		}
		if !f.IsExported() {
			continue
		}
		tag := f.Tag.Get("json")
		name, opts, _ := strings.Cut(tag, ",")
		if tag == "-" {
			continue
		}
		if strings.Contains(","+opts+",", ",string,") {
			return no
		}
		if name == "" || strings.Trim(name, "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
			// encoding/json falls back to the field name on an invalid tag
			// name too; only plain identifiers are taken here.
			if name != "" {
				return no
			}
			name = f.Name
		}
		if _, dup := byName[name]; dup {
			return no
		}
		byName[name] = f.Index
		names = append(names, name)
	}
	p := &directPlan{ok: true, fields: make([]directField, len(cols))}
	for ci, c := range cols {
		idx, ok := byName[c]
		if !ok {
			for _, n := range names {
				if strings.EqualFold(n, c) {
					return no // encoding/json would match it case-insensitively
				}
			}
			continue // no field: JSON ignores the key
		}
		ft := t.FieldByIndex(idx).Type
		var k directKind
		switch {
		case ft.Kind() == reflect.String:
			k = dkString
		case ft.Kind() == reflect.Int64 || ft.Kind() == reflect.Int:
			k = dkInt
		case ft.Kind() == reflect.Float64:
			k = dkFloat
		case ft.Kind() == reflect.Pointer && ft.Elem().Kind() == reflect.String && ft.Elem() == reflect.TypeFor[string]():
			k = dkPtrString
		case ft.Kind() == reflect.Pointer && ft.Elem() == reflect.TypeFor[int64]():
			k = dkPtrInt64
		default:
			return no
		}
		if (k == dkString && ft != reflect.TypeFor[string]()) || (k == dkInt && ft != reflect.TypeFor[int64]() && ft != reflect.TypeFor[int]()) || (k == dkFloat && ft != reflect.TypeFor[float64]()) {
			return no // a named type (TextUnmarshaler and friends): leave it to JSON
		}
		p.fields[ci] = directField{index: idx, kind: k}
	}
	return p
}

// decodeRowsDirect decodes rows into a fresh []T, or reports ok=false
// without having changed anything the caller can see.
func decodeRowsDirect[T any](cols []string, rows [][]any) ([]T, bool) {
	p := directPlanFor(reflect.TypeFor[T](), cols)
	if !p.ok {
		return nil, false
	}
	out := make([]T, len(rows)) // non-nil even when empty: JSON's "[]"
	for ri, row := range rows {
		v := reflect.ValueOf(&out[ri]).Elem()
		for ci, f := range p.fields {
			if f.kind == dkSkip {
				continue
			}
			switch x := row[ci].(type) {
			case nil:
				// JSON null: a no-op on a value, nil (already) on a pointer.
			case string:
				s := jsonString(x)
				switch f.kind {
				case dkString:
					v.FieldByIndex(f.index).SetString(s)
				case dkPtrString:
					v.FieldByIndex(f.index).Set(reflect.ValueOf(&s))
				default:
					return nil, false
				}
			case int64:
				switch f.kind {
				case dkInt:
					v.FieldByIndex(f.index).SetInt(x)
				case dkPtrInt64:
					n := x
					v.FieldByIndex(f.index).Set(reflect.ValueOf(&n))
				default:
					return nil, false
				}
			case float64:
				if f.kind != dkFloat {
					return nil, false
				}
				v.FieldByIndex(f.index).SetFloat(x)
			default:
				return nil, false
			}
		}
	}
	return out, true
}

// jsonString is s as a json.Marshal / json.Unmarshal round trip leaves it:
// each byte of an invalid UTF-8 sequence becomes U+FFFD.
func jsonString(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	var b strings.Builder
	for i := 0; i < len(s); {
		r, size := utf8.DecodeRuneInString(s[i:])
		if r == utf8.RuneError && size == 1 {
			b.WriteRune(utf8.RuneError)
		} else {
			b.WriteString(s[i : i+size])
		}
		i += size
	}
	return b.String()
}
