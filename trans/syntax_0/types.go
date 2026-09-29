package syntax_0

import (
	"fmt"
	"slices"
	"strings"

	"gd++/trans/meta"
)

// builtins are Godot's non-object types, with their C++ types where the name differs.
var builtins = map[string]string{
	"bool": "bool", "int": "int64_t", "float": "double", "Variant": "Variant",
	"String": "", "StringName": "", "NodePath": "", "RID": "", "Callable": "", "Signal": "",
	"Vector2": "", "Vector2i": "", "Vector3": "", "Vector3i": "", "Vector4": "", "Vector4i": "",
	"Rect2": "", "Rect2i": "", "Transform2D": "", "Transform3D": "", "Plane": "", "Quaternion": "",
	"AABB": "", "Basis": "", "Projection": "", "Color": "", "Dictionary": "", "Array": "",
	"PackedByteArray": "", "PackedInt32Array": "", "PackedInt64Array": "", "PackedFloat32Array": "",
	"PackedFloat64Array": "", "PackedStringArray": "", "PackedVector2Array": "", "PackedVector3Array": "",
	"PackedVector4Array": "", "PackedColorArray": "",
}

// symbol is a class, extern or enum type that GD++ code can name: a dependency or a declaration in the file.
type symbol struct {
	name    string
	kind    meta.Kind
	include string // Empty for declarations in the file.
	cpp     string // For dependencies named differently in C++, e.g. "::core_bind::OS".
	values  []meta.EnumValue
	class   *Class  // Set for classes in the file.
	extern  *Extern // Set for externs in the file.
	enum    *Enum   // Set for enums in the file.
}

// gtype is a resolved GD++ type.
type gtype struct {
	cpp   string  // The C++ type, e.g. "Ref<Resource>".
	doc   string  // Godot's doc spelling, e.g. "Resource" or "int[]".
	byRef bool    // Whether parameters take it as const &.
	enum  *symbol // Set for enums.
	void  bool
}

// param is the C++ parameter type of t.
func (t *gtype) param() string {
	if t.byRef {
		return "const " + t.cpp + " &"
	}
	return t.cpp
}

var variantType = &gtype{cpp: "Variant", doc: "Variant", byRef: true}

// resolve returns the type named by t, or nil for a missing type (a Variant). void is allowed only if allowVoid.
func (u *unit) resolve(t *Type, allowVoid bool) (*gtype, error) {
	if t == nil {
		return variantType, nil
	}
	if t.Name == "void" {
		if !allowVoid || len(t.Args) > 0 {
			return nil, u.errorAt(t.Pos, len(t.Name), "Type void is only allowed as a return type.", "")
		}
		return &gtype{cpp: "void", doc: "void", void: true}, nil
	}
	if (t.Name == "Array" && len(t.Args) > 1) || (t.Name == "Dictionary" && len(t.Args) != 0 && len(t.Args) != 2) ||
		(len(t.Args) > 0 && t.Name != "Array" && t.Name != "Dictionary") {
		return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Type %s doesn't take these type arguments.", t.Name),
			"Only Array[T] and Dictionary[K, V] take type arguments.")
	}
	if len(t.Args) > 0 {
		var cpp, doc []string
		for _, arg := range t.Args {
			a, err := u.resolveElement(arg)
			if err != nil {
				return nil, err
			}
			cpp, doc = append(cpp, a.cpp), append(doc, a.doc)
		}
		if t.Name == "Array" {
			return &gtype{cpp: "TypedArray<" + cpp[0] + ">", doc: doc[0] + "[]", byRef: true}, nil
		}
		return &gtype{cpp: "TypedDictionary<" + strings.Join(cpp, ", ") + ">", doc: "Dictionary[" + strings.Join(doc, ", ") + "]", byRef: true}, nil
	}
	if cpp, ok := builtins[t.Name]; ok {
		if cpp == "" {
			cpp = t.Name
		}
		return &gtype{cpp: cpp, doc: t.Name, byRef: !slices.Contains([]string{"bool", "int", "float"}, t.Name)}, nil
	}
	s := u.symbols[t.Name]
	if s == nil {
		return nil, u.unknownType(t)
	}
	switch s.kind {
	case meta.Object:
		return &gtype{cpp: s.name + " *", doc: s.name}, nil
	case meta.RefCounted:
		return &gtype{cpp: "Ref<" + s.name + ">", doc: s.name, byRef: true}, nil
	case meta.Extern:
		return &gtype{cpp: "gdpp::ExtPtr<" + s.name + ">", doc: s.name}, nil
	case meta.RefCountedExtern:
		return &gtype{cpp: "gdpp::ExtRef<" + s.name + ">", doc: s.name}, nil
	case meta.Enum:
		return &gtype{cpp: s.name, doc: "int", enum: s}, nil
	}
	return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("%s is not a Godot type.", t.Name),
		"Types are Godot's built-in types and classes, and the package's classes, externs and enums.")
}

// resolveElement resolves a type argument of Array or Dictionary.
func (u *unit) resolveElement(t *Type) (*gtype, error) {
	if len(t.Args) > 0 {
		return nil, u.errorAt(t.Pos, len(t.Name), "Typed collections can't be nested.", "Use a plain Array or Dictionary inside.")
	}
	if s := u.symbols[t.Name]; s != nil && s.kind != meta.Object && s.kind != meta.RefCounted {
		return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Type %s can't be used in a typed collection.", t.Name),
			"Typed collections can hold built-in types and classes, but not enums or externs.")
	}
	g, err := u.resolve(t, false)
	if err != nil || builtins[t.Name] != "" || u.symbols[t.Name] == nil {
		return g, err
	}
	return &gtype{cpp: t.Name, doc: t.Name}, nil // TypedArray<Node>, not TypedArray<Node *>.
}

// unknownType returns the error for the unknown type t.
func (u *unit) unknownType(t *Type) error {
	return u.unknownName(t, "type")
}

// unknownName returns the error for t, an unknown type used as what, e.g. "base class".
func (u *unit) unknownName(t *Type, what string) error {
	var names []string
	for name := range builtins {
		names = append(names, name)
	}
	for name := range u.symbols {
		names = append(names, name)
	}
	hint := "Types are Godot types, or classes, externs and enums from the dependencies or this file."
	if s := suggest(t.Name, names...); s != "" {
		hint = fmt.Sprintf("Did you mean %q?", s)
	}
	return u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Unknown %s %q.", what, t.Name), hint)
}

// upperSnake converts PascalCase to UPPER_SNAKE_CASE, e.g. "HTTPCode" to "HTTP_CODE".
func upperSnake(s string) string {
	var b strings.Builder
	for i, r := range s {
		isUpper := func(j int) bool { return j >= 0 && j < len(s) && s[j] >= 'A' && s[j] <= 'Z' }
		if i > 0 && isUpper(i) && (!isUpper(i-1) || i+1 < len(s) && !isUpper(i+1) && s[i+1] != '_') && s[i-1] != '_' {
			b.WriteByte('_')
		}
		b.WriteRune(r)
	}
	return strings.ToUpper(b.String())
}
