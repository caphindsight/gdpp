package syntax_0

import (
	"fmt"
	"reflect"
	"strconv"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
)

// Dump renders an AST node as indented, YAML-like text. Each struct shows its position as @line:col.
// Empty fields are left out, and a struct with a single one-line value is printed on one line.
func Dump(node any) string {
	var sb strings.Builder
	dumpValue(&sb, reflect.TypeOf(node).Elem().Name()+": ", reflect.ValueOf(node), "")
	return sb.String()
}

// dumpValue writes v with the given key prefix ("Name: " or "- ").
func dumpValue(sb *strings.Builder, key string, v reflect.Value, indent string) {
	if isEmpty(v) {
		return
	}
	switch v.Kind() {
	case reflect.Pointer:
		dumpValue(sb, key, v.Elem(), indent)
	case reflect.Struct:
		if pos, ok := v.Interface().(lexer.Position); ok {
			fmt.Fprintf(sb, "%s%s%d:%d\n", indent, key, pos.Line, pos.Column)
			return
		}
		pos := v.FieldByName("Pos").Interface().(lexer.Position)
		fmt.Fprintf(sb, "%s%s@%d:%d", indent, key, pos.Line, pos.Column)
		var fields []int
		for i := 0; i < v.NumField(); i++ {
			if f := v.Type().Field(i); f.Name != "Pos" && f.Tag.Get("dump") != "-" && !isEmpty(v.Field(i)) {
				fields = append(fields, i)
			}
		}
		if len(fields) == 1 {
			if s, ok := scalar(v.Field(fields[0])); ok && !strings.Contains(s, "\n") {
				fmt.Fprintf(sb, " %s\n", s)
				return
			}
		}
		sb.WriteString("\n")
		for _, i := range fields {
			dumpValue(sb, v.Type().Field(i).Name+": ", v.Field(i), indent+"  ")
		}
	case reflect.Slice:
		fmt.Fprintf(sb, "%s%s\n", indent, strings.TrimSuffix(key, " "))
		for i := 0; i < v.Len(); i++ {
			dumpValue(sb, "- ", v.Index(i), indent+"  ")
		}
	default:
		s, _ := scalar(v)
		if !strings.Contains(s, "\n") {
			fmt.Fprintf(sb, "%s%s%s\n", indent, key, s)
			return
		}
		fmt.Fprintf(sb, "%s%s|\n", indent, key)
		for _, line := range strings.Split(s, "\n") {
			if line != "" {
				line = indent + "  " + line
			}
			sb.WriteString(line + "\n")
		}
	}
}

// scalar formats a string, bool or integer value.
func scalar(v reflect.Value) (string, bool) {
	switch v.Kind() {
	case reflect.String:
		if s := v.String(); s != strings.TrimSpace(s) && !strings.Contains(s, "\n") {
			return strconv.Quote(s), true // Make leading and trailing spaces visible.
		}
		return v.String(), true
	case reflect.Bool, reflect.Int64:
		return fmt.Sprint(v.Interface()), true
	}
	return "", false
}

func isEmpty(v reflect.Value) bool {
	switch v.Kind() {
	case reflect.Pointer:
		return v.IsNil()
	case reflect.Slice:
		return v.Len() == 0
	case reflect.String:
		return v.String() == ""
	case reflect.Bool:
		return !v.Bool()
	}
	return false
}
