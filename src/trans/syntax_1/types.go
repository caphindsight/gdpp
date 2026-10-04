package syntax_1

import (
	"cmp"
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
	name          string
	kind          meta.Kind
	include       string // What follows #include. Declarations in the file are in "<Name>.h".
	gdpp          bool   // Whether a GD++ file declares it: this one or another.
	values        []meta.EnumValue
	base          string   // For enums: the enum it extends, until enumValues adds the base's values to values. For dependency classes and externs: their base, if known.
	bitfield      bool     // For enums: whether its values are flags.
	godotNames    []string // For engine enums: Godot's name of each value, e.g. SHADOW_CASTING_SETTING_ON for ON.
	virtuals      []string // For dependency classes: the names of their virtual functions, see meta.Dependency.
	notifications []string // For dependency classes: the names of their own notifications, see meta.Dependency.
	nonRuntime    bool     // For dependency classes: see meta.Dependency.
	class         *Class   // Set for classes in the file.
	extern        *Extern  // Set for externs in the file.
	enum          *Enum    // Set for enums in the file.
}

// local reports whether the file declares s.
func (s *symbol) local() bool {
	return s.class != nil || s.extern != nil || s.enum != nil
}

// isExtern reports whether s is an extern: of the file or a dependency.
func (s *symbol) isExtern() bool {
	return s.extern != nil || s.kind == meta.Extern || s.kind == meta.RefCountedExtern
}

// gtype is a resolved GD++ type.
type gtype struct {
	cpp   string  // The C++ type, e.g. "Ref<Resource>".
	doc   string  // Godot's doc spelling, e.g. "Resource" or "int[]".
	byRef bool    // Whether parameters take it as const &.
	enum  *symbol // Set for enums.
	async *gtype  // For Async types: the type of the result.
	weak  bool    // Whether it's a Weak type.
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
	if t.Name == "Async" {
		if len(t.Args) > 1 {
			return nil, u.errorAt(t.Pos, len(t.Name), "Type Async takes one type argument, the type of its result, e.g. Async[int].", "")
		}
		result := variantType
		if len(t.Args) == 1 {
			var err error
			if t.Args[0].Name == "Async" {
				return nil, u.errorAt(t.Args[0].Pos, len(t.Args[0].Name), "An Async can't have an Async result.", "Use the inner type, e.g. Async[int].")
			}
			if result, err = u.resolve(t.Args[0], true); err != nil {
				return nil, err
			}
		}
		return u.async(result), nil
	}
	if t.Name == "Weak" {
		return u.weak(t)
	}
	if (t.Name == "Array" && len(t.Args) > 1) || (t.Name == "Dictionary" && len(t.Args) != 0 && len(t.Args) != 2) ||
		(len(t.Args) > 0 && t.Name != "Array" && t.Name != "Dictionary") {
		return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Type %s doesn't take these type arguments.", t.Name),
			"Only Array[T], Dictionary[K, V], Async[T] and Weak[T] take type arguments.")
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
	case meta.GodotEnum:
		return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("%s is not a GD++ type.", t.Name),
			fmt.Sprintf("Use int, or redefine it as a GD++ enum: \"enum My%s { extends %s }\".", t.Name, t.Name))
	}
	return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("%s is not a Godot type.", t.Name),
		"Types are Godot's built-in types and classes, and the package's classes, externs and enums.")
}

// weak resolves t, a Weak type: a reference to an object of a class, which doesn't keep it alive.
func (u *unit) weak(t *Type) (*gtype, error) {
	if len(t.Args) != 1 {
		return nil, u.errorAt(t.Pos, len(t.Name), "Type Weak takes one type argument, a class, e.g. Weak[Node3D].", "")
	}
	arg := t.Args[0]
	if _, err := u.resolve(arg, false); err != nil {
		return nil, err
	}
	if s := u.symbols[arg.Name]; s != nil && (s.kind == meta.Object || s.kind == meta.RefCounted) {
		return &gtype{cpp: "gdpp::Weak<" + s.name + ">", doc: s.name, weak: true}, nil
	}
	return nil, u.errorAt(arg.Pos, len(arg.Name), fmt.Sprintf("Weak needs a class, but %s isn't one.", arg.Name),
		"E.g. Weak[Node3D], Weak[Resource], or Weak of a GD++ class.")
}

// async returns the Async type whose result has type result.
func (u *unit) async(result *gtype) *gtype {
	return &gtype{cpp: "gdpp::Async<" + result.cpp + ">", doc: cmp.Or(u.opts.AsyncClass, "GdppAsync"), async: result}
}

// asyncArg returns the type argument of t, an Async type, or nil (a Variant) if it has none.
func asyncArg(t *Type) *Type {
	if len(t.Args) == 0 {
		return nil
	}
	return t.Args[0]
}

// resolveElement resolves a type argument of Array or Dictionary.
func (u *unit) resolveElement(t *Type) (*gtype, error) {
	if t.Name == "Async" || t.Name == "Weak" {
		return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Typed collections can't hold %s values.", t.Name), "Use a plain Array or Dictionary.")
	}
	if len(t.Args) > 0 {
		return nil, u.errorAt(t.Pos, len(t.Name), "Typed collections can't be nested.", "Use a plain Array or Dictionary inside.")
	}
	_, builtin := builtins[t.Name]
	if s := u.symbols[t.Name]; !builtin && s != nil && s.kind != meta.Object && s.kind != meta.RefCounted {
		return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Type %s can't be used in a typed collection.", t.Name),
			"Typed collections can hold built-in types and classes, but not enums or externs.")
	}
	g, err := u.resolve(t, false)
	if err != nil || builtin || u.symbols[t.Name] == nil {
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
	names := []string{"Async", "Weak"}
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
