package syntax_1

import (
	"fmt"
	"strings"
)

// structModel is a checked struct: a C++ value type, which Godot sees as a Dictionary of its fields, and of Callables
// of its funcs, those without @static or @private.
type structModel struct {
	name   string
	st     *Struct
	fields []*varModel
	funcs  []*funcModel
	codes  []*Code // decl and impl blocks.
}

// fileStructs returns the structs that file declares: the file-level one first.
func fileStructs(file *File) []*Struct {
	if file.FileStruct != nil {
		return append([]*Struct{file.FileStruct}, file.InlineStructs...)
	}
	return file.InlineStructs
}

// exposed reports whether the Dictionary of the struct has a Callable of f.
func (f *funcModel) exposed() bool {
	return !f.static && !f.isPrivate
}

// buildStructs checks the structs of the file.
func (u *unit) buildStructs() error {
	for _, st := range fileStructs(u.file) {
		m, err := u.buildStruct(st)
		if err != nil {
			return err
		}
		u.structs = append(u.structs, m)
	}
	return nil
}

func (u *unit) buildStruct(st *Struct) (*structModel, error) {
	if _, err := u.annotations(st.Annotations, "a struct"); err != nil {
		return nil, err
	}
	m := &structModel{name: st.Name, st: st}
	names := map[string]bool{}
	for _, member := range st.Members {
		keyword, pos := member.keyword()
		name := ""
		switch {
		case member.Var != nil:
			v := member.Var
			if _, err := u.annotations(v.Annotations, "a struct var"); err != nil {
				return nil, err
			}
			if v.Property != nil {
				return nil, u.errorAt(v.Pos, 3, fmt.Sprintf("Struct %s's var %s can't be a property.", st.Name, v.Name),
					"Struct vars are plain fields. To compute a value, write a func, e.g. \"@const func ...\".")
			}
			if init := v.Init; init != nil && init.Block == nil && (strings.HasPrefix(init.Expr, "$") || strings.HasPrefix(init.Expr, "%")) {
				return nil, u.errorAt(init.Pos, len(init.Expr), "A struct var can't have a node path.", "Only @onready vars of nodes get nodes by their paths.")
			}
			t, err := u.resolve(v.Type, false)
			if err != nil {
				return nil, err
			}
			if err := u.structType(t, v.Type, st.Name, "a var"); err != nil {
				return nil, err
			}
			if t.strukt != nil && t.strukt.name == st.Name {
				return nil, u.errorAt(v.Type.Pos, len(v.Type.Name), fmt.Sprintf("Struct %s can't contain itself.", st.Name),
					"A struct holds its fields by value. Use a class instead, which can refer to itself through a Gd.")
			}
			if err := u.enumShorthand(t, v.Init); err != nil {
				return nil, err
			}
			m.fields = append(m.fields, &varModel{v: v, t: t})
			name = v.Name
		case member.Func != nil:
			f := member.Func
			if _, err := u.annotations(f.Annotations, "a struct func", "const", "private", "static"); err != nil {
				return nil, err
			}
			if f.Body == nil {
				return nil, u.errorAt(f.Pos, 4, fmt.Sprintf("Struct %s's func %s needs a body.", st.Name, f.Name), "Structs define their funcs.")
			}
			fm, err := u.buildFunc(f, st.Name, false)
			if err != nil {
				return nil, err
			}
			for i, p := range f.Params {
				if err := u.structType(fm.params[i], p.Type, st.Name, "a func parameter"); err != nil {
					return nil, err
				}
			}
			if err := u.structType(fm.ret, f.Return, st.Name, "a func"); err != nil {
				return nil, err
			}
			m.funcs = append(m.funcs, fm)
			name = f.Name
		case member.Code != nil && !member.Code.Shader:
			if _, err := u.annotations(member.Code.Annotations, "a "+keyword+" block in a struct"); err != nil {
				return nil, err
			}
			m.codes = append(m.codes, member.Code)
			continue
		default:
			return nil, u.errorAt(pos, len(keyword), fmt.Sprintf("Structs can't contain %s.", map[string]string{"ctor": "a ctor", "dtor": "a dtor",
				"on": "on blocks", "enum": "enums", "import": "imports", "noimport": "imports", "signal": "signals", "shader": "shaders or shader blocks"}[keyword]),
				"Structs contain vars, funcs, and decl and impl blocks.")
		}
		if strings.HasPrefix(name, "_gdpp_") {
			return nil, u.errorAt(pos, len(keyword), "Names that start with _gdpp_ are reserved for generated code.", "")
		}
		if err := u.unique(names, pos, keyword, name); err != nil {
			return nil, err
		}
	}
	return m, nil
}

// structType reports t, which typ names, if a member of struct owner, what, can't have it: the struct converts every
// value of its fields and funcs to and from a Variant.
func (u *unit) structType(t *gtype, typ *Type, owner, what string) error {
	if t.async == nil && !t.weak && t.gpu == "" {
		return nil
	}
	return u.errorAt(typ.Pos, len(typ.Name), fmt.Sprintf("Struct %s can't have %s of type %s.", owner, what, typeString(typ)),
		"Structs hold values that convert to and from a Variant. Async, Weak and GpuArray values don't.")
}

// structDecl writes the definition of struct s, and its bindings.
func (u *unit) structDecl(w *writer, s *structModel) {
	w.ln("")
	w.ln("#define This %s", s.name)
	w.ln("struct %s {", s.name)
	for _, v := range s.fields {
		switch init := v.v.Init; {
		case init == nil:
			w.ln("\t%s %s{};", v.t.cpp, v.v.Name)
		case init.Block != nil:
			w.block(init.Block, fmt.Sprintf("\t%s %s = [&]() -> %s {", v.t.cpp, v.v.Name, v.t.cpp), "}();", assertValue)
		default:
			w.user(init.Pos, init.Origin, fmt.Sprintf("\t%s %s = ", v.t.cpp, v.v.Name), init.Expr, ";", assertValue)
		}
	}
	if len(s.funcs) > 0 {
		w.ln("")
	}
	for _, f := range s.funcs {
		decl := withSpace(f.ret.cpp) + f.f.Name + "(" + declParams(f) + ")"
		if f.static {
			decl = "static " + decl
		}
		if f.isConst {
			decl += " const"
		}
		w.ln("\t%s;", decl)
	}
	w.ln("")
	w.ln("\toperator Variant() const { return _gdpp_to_dictionary(); }")
	w.ln("\t// The Dictionary of the fields, which scenes and RPCs use.")
	w.ln("\tDictionary _gdpp_fields() const;")
	w.ln("\t// The Dictionary of the fields, and of Callables of the funcs, which share a copy of the struct.")
	w.ln("\tDictionary _gdpp_to_dictionary() const;")
	w.ln("\t// The struct with the fields of a Dictionary, and the default values of the others.")
	w.ln("\tstatic %s _gdpp_from(const Variant &p_value);", s.name)
	var private []string
	for _, f := range s.funcs {
		for i, p := range f.f.Params {
			if p.Default != nil {
				private = append(private, fmt.Sprintf("static %s%s();", withSpace(f.params[i].cpp), defaultName(f, p)))
			}
		}
	}
	if len(private) > 0 {
		w.ln("")
		w.ln("private:")
		for _, d := range private {
			w.ln("\t%s", d)
		}
	}
	for _, code := range s.codes {
		if code.Decl {
			w.ln("")
			w.ln("private:")
			w.block(code.Body, "", "", assertAny)
		}
	}
	w.ln("};")
	w.ln("#undef This")
	w.ln("GDPP_STRUCT(%s)", s.name)
}

// structDefs writes the definitions of struct s's funcs and conversions, and its impl blocks.
func (u *unit) structDefs(w *writer, s *structModel) {
	c := &classModel{name: s.name} // What qualified and defaultDefs need.
	w.ln("")
	w.ln("#define This %s", s.name)
	for _, f := range s.funcs {
		defaultDefs(w, c, f)
		w.ln("")
		w.ln("%s {", qualified(c, f.ret.cpp, f.f.Name, params(nil, f.params, f.f.Params), f.isConst))
		w.static = f.static
		w.block(f.f.Body, "", "", assertFor(f.ret.void))
		w.static = false
		w.ln("}")
	}
	// value returns the field v as a Variant, a nested struct with its funcs' Callables if callables.
	value := func(v *varModel, callables bool) string {
		switch {
		case v.t.enum != nil:
			return "static_cast<int64_t>(" + v.v.Name + ")"
		case v.t.strukt != nil && callables:
			return v.v.Name + "._gdpp_to_dictionary()"
		case v.t.strukt != nil:
			return v.v.Name + "._gdpp_fields()"
		}
		return v.v.Name
	}
	for _, callables := range []bool{false, true} {
		w.ln("")
		w.ln("Dictionary %s::%s() const {", s.name, map[bool]string{false: "_gdpp_fields", true: "_gdpp_to_dictionary"}[callables])
		w.ln("\tDictionary result;")
		for _, v := range s.fields {
			w.ln("\tresult[%q] = %s;", v.v.Name, value(v, callables))
		}
		exposed := false
		for _, f := range s.funcs {
			if callables && f.exposed() {
				if !exposed {
					w.ln("\tauto self = std::make_shared<%s>(*this);", s.name)
					exposed = true
				}
				w.ln("\tresult[%q] = gdpp::struct_callable(self, &%s::%s);", f.f.Name, s.name, f.f.Name)
			}
		}
		w.ln("\treturn result;")
		w.ln("}")
	}
	var keys []string
	for _, v := range s.fields {
		keys = append(keys, fmt.Sprintf("%q", v.v.Name))
	}
	for _, f := range s.funcs {
		if f.exposed() {
			keys = append(keys, fmt.Sprintf("%q", f.f.Name))
		}
	}
	w.ln("")
	w.ln("%s %s::_gdpp_from(const Variant &p_value) {", s.name, s.name)
	w.ln("\tDictionary dict = gdpp::struct_dictionary(p_value, %q, { %s });", s.name, strings.Join(keys, ", "))
	w.ln("\t%s result;", s.name)
	for _, v := range s.fields {
		w.ln("\tgdpp::struct_field(dict, %q, result.%s, %q);", v.v.Name, v.v.Name, s.name)
	}
	w.ln("\treturn result;")
	w.ln("}")
	for _, code := range s.codes {
		if code.Impl {
			w.ln("")
			w.block(code.Body, "", "", assertAny)
		}
	}
	w.ln("")
	w.ln("#undef This")
}
