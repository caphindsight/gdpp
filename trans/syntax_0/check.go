package syntax_0

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"

	"gd++/trans/meta"
)

// unit is a checked GD++ file, ready for code generation.
type unit struct {
	src     string
	file    *File
	opts    meta.Options
	symbols map[string]*symbol // Types that code can name, except built-ins.
	enums   []*symbol          // Enum types declared in the file.
	consts  []*Enum            // Integer constants outside classes.
	classes []*classModel      // Bases first.
	externs []*externModel
	topCode []*Code // decl and impl blocks outside classes.
}

type classModel struct {
	name       string
	cls        *Class
	base       string
	refCounted bool
	tool       bool // Whether its functions run in the editor too.
	codes      []*Code
	ctor, dtor *Block
	funcs      []*funcModel
	vars       []*varModel
	signals    []*signalModel
	consts     []*Enum
	enums      []*symbol // Enums the class exposes its own copy of.
	imports    []*Type
	noimports  []*Type
}

type externModel struct {
	name       string
	ext        *Extern
	base       string
	refCounted bool
	funcs      []*funcModel
	vars       []*varModel
	signals    []*signalModel
}

type funcModel struct {
	f                                            *Func
	params                                       []*gtype
	ret                                          *gtype
	virtual, override, isConst, static, deferred bool
	rpc                                          *rpcModel // Nil without @rpc.
}

// rpcModel is the configuration from @rpc, as C++ values. Empty in externs, whose defining class configures it.
type rpcModel struct {
	mode, transfer string // E.g. "MultiplayerAPI::RPC_MODE_ANY_PEER".
	callLocal      bool
	channel        string
}

// usesEnums reports whether the function's signature mentions an enum, so it's bound through a trampoline.
func (f *funcModel) usesEnums() bool {
	return f.ret.enum != nil || slices.ContainsFunc(f.params, func(t *gtype) bool { return t.enum != nil })
}

type varModel struct {
	v                *Var
	t                *gtype
	onready          bool
	usage            string // A PROPERTY_USAGE_* expression.
	hint, hintString string // A PROPERTY_HINT_* name, and the hint string (not quoted).
	getter, setter   string // Empty if there is none.
	get              *Block
	set              *Setter
	decls            []*Block
	sections         []section // In source order.
}

// section is an inspector section a var starts, from a section annotation.
type section struct {
	ann          *Annotation
	name, prefix string
}

type signalModel struct {
	s      *Signal
	params []*gtype
}

func (u *unit) errorAt(pos lexer.Position, n int, msg, hint string) *Error {
	return (&Error{Pos: pos, Len: max(n, 1), Msg: msg, Hint: hint}).withSource(u.src)
}

// parseUnit parses src and indexes the declarations, without dependencies.
func parseUnit(filename, src string) (*unit, error) {
	file, err := Parse(filename, src)
	if err != nil {
		return nil, err
	}
	u := &unit{src: src, file: file, symbols: map[string]*symbol{}}
	declare := func(pos lexer.Position, name string, s *symbol) error {
		if u.symbols[name] != nil {
			return u.errorAt(pos, 0, fmt.Sprintf("The name %q is declared twice in this file.", name), "Class, extern and enum names must be unique in the package.")
		}
		s.name = name
		u.symbols[name] = s
		return nil
	}
	classes := file.InlineClasses
	if file.FileClass != nil {
		classes = append([]*Class{file.FileClass}, classes...)
	}
	for _, c := range classes {
		if err := declare(c.Pos, c.Name, &symbol{class: c}); err != nil {
			return nil, err
		}
	}
	externs := file.InlineExterns
	if file.FileExtern != nil {
		externs = append([]*Extern{file.FileExtern}, externs...)
	}
	for _, e := range externs {
		if err := declare(e.Pos, e.Name, &symbol{extern: e}); err != nil {
			return nil, err
		}
	}
	enums := file.InlineEnums
	if file.FileEnum != nil {
		enums = append([]*Enum{file.FileEnum}, enums...)
	}
	for _, c := range classes {
		for _, m := range c.Members {
			if m.Enum != nil {
				enums = append(enums, m.Enum)
			}
		}
	}
	for _, e := range enums {
		if e.Value != nil {
			if !slices.Contains(file.InlineEnums, e) {
				continue // A class constant.
			}
			u.consts = append(u.consts, e)
			continue
		}
		s := &symbol{kind: meta.Enum, enum: e}
		if err := declare(e.Pos, e.Name, s); err != nil {
			return nil, err
		}
		next := int64(0)
		for _, entry := range e.Entries {
			if entry.Value != nil {
				next = entry.Value.Value
			}
			if slices.ContainsFunc(s.values, func(v meta.EnumValue) bool { return v.Name == entry.Name }) {
				return nil, u.errorAt(entry.Pos, len(entry.Name), fmt.Sprintf("Enum %s has two values named %s.", e.Name, entry.Name), "")
			}
			doc := ""
			if entry.Doc != nil {
				doc = entry.Doc.Text
			}
			s.values = append(s.values, meta.EnumValue{Name: entry.Name, Value: next, Doc: doc})
			next++
		}
		u.enums = append(u.enums, s)
	}
	return u, nil
}

// declarations lists what the file declares, for other files.
func (u *unit) declarations() ([]meta.Declaration, error) {
	var decls []meta.Declaration
	for _, s := range u.sortedSymbols() {
		switch {
		case s.class != nil:
			icon, err := u.classIcon(s.class)
			if err != nil {
				return nil, err
			}
			decls = append(decls, meta.Declaration{Name: s.name, Kind: meta.ClassDecl, Base: baseName(s.class.Extends), Icon: icon,
				Tool: slices.ContainsFunc(s.class.Annotations, func(a *Annotation) bool { return a.Name == "tool" })})
		case s.extern != nil:
			decls = append(decls, meta.Declaration{Name: s.name, Kind: meta.ExternDecl, Base: baseName(s.extern.Extends)})
		default:
			decls = append(decls, meta.Declaration{Name: s.name, Kind: meta.EnumDecl, Values: s.values})
		}
	}
	return decls, nil
}

// classIcon returns the path from the @icon annotation of class c, or "" if it has none.
func (u *unit) classIcon(c *Class) (string, error) {
	for _, a := range c.Annotations {
		if a.Name != "icon" {
			continue
		}
		if len(a.Args) != 1 || !strings.HasPrefix(a.Args[0].Value, "\"") {
			return "", u.errorAt(a.Pos, len(a.Name)+1, "Annotation @icon needs one argument: the icon's path, as a string.",
				"E.g. \"@icon(\\\"res://icons/player.svg\\\")\".")
		}
		arg := a.Args[0]
		path, err := strconv.Unquote(arg.Value)
		if err != nil || !strings.HasPrefix(path, "res://") && !strings.HasPrefix(path, "pkg://") {
			return "", u.errorAt(arg.Pos, len(arg.Value), "The icon's path must start with res:// or pkg://.",
				"res:// paths are relative to the project, pkg:// paths to the package.")
		}
		return path, nil
	}
	return "", nil
}

// sortedSymbols returns the file's declarations in source order.
func (u *unit) sortedSymbols() []*symbol {
	var syms []*symbol
	for _, s := range u.symbols {
		if s.class != nil || s.extern != nil || s.enum != nil {
			syms = append(syms, s)
		}
	}
	slices.SortFunc(syms, func(a, b *symbol) int { return a.pos().Offset - b.pos().Offset })
	return syms
}

func (s *symbol) pos() lexer.Position {
	switch {
	case s.class != nil:
		return s.class.Pos
	case s.extern != nil:
		return s.extern.Pos
	}
	return s.enum.Pos
}

func baseName(t *Type) string {
	if t == nil {
		return "RefCounted"
	}
	return t.Name
}

// newUnit parses src and checks it against the dependencies in opts.
func newUnit(filename, src string, opts meta.Options) (*unit, error) {
	u, err := parseUnit(filename, src)
	if err != nil {
		return nil, err
	}
	base := filepath.Base(filename)
	for _, ext := range []string{".gd++", ".gdpp", ".gg"} {
		base = strings.TrimSuffix(base, ext)
	}
	if opts.SourceName == "" {
		opts.SourceName = filename
	}
	if opts.HeaderName == "" {
		opts.HeaderName = base + ".h"
	}
	if opts.CodeName == "" {
		opts.CodeName = base + ".cpp"
	}
	u.opts = opts
	for _, d := range opts.Dependencies {
		if s := u.symbols[d.Name]; s != nil {
			if s.include != "" {
				continue // A duplicate dependency.
			}
			return nil, u.errorAt(s.pos(), 0, fmt.Sprintf("The name %q is already declared by a dependency.", d.Name),
				"Names must differ from Godot's, and from those of the package's other classes, externs and enums.")
		}
		u.symbols[d.Name] = &symbol{name: d.Name, kind: d.Kind, include: d.Include, values: d.Values, gdpp: d.Gdpp}
	}
	for _, s := range u.sortedSymbols() {
		if s.include == "" && s.kind == 0 {
			if _, err := u.kindOf(s, nil); err != nil {
				return nil, err
			}
		}
	}
	u.topCode = u.file.Code
	if err := u.buildExterns(); err != nil {
		return nil, err
	}
	if err := u.buildClasses(); err != nil {
		return nil, err
	}
	return u, nil
}

// kindOf computes the kind of the class or extern s from its base chain.
func (u *unit) kindOf(s *symbol, seen []*symbol) (meta.Kind, error) {
	if s.kind != 0 {
		return s.kind, nil
	}
	ext := s.extern != nil
	var extends *Type
	if ext {
		extends = s.extern.Extends
	} else {
		extends = s.class.Extends
	}
	name := baseName(extends)
	pos := s.pos()
	if extends != nil {
		pos = extends.Pos
	}
	b := u.symbols[name]
	switch {
	case slices.Contains(seen, s):
		return 0, u.errorAt(pos, len(name), fmt.Sprintf("Class %s extends itself through its bases.", s.name), "")
	case b == nil:
		t := &Type{Pos: pos, Name: name}
		if extends == nil {
			return 0, u.errorAt(pos, 0, fmt.Sprintf("Unknown base class \"RefCounted\" of %s.", s.name), "Add RefCounted to the dependencies, or extend another class.")
		}
		return 0, u.unknownName(t, "base class")
	case b.kind == meta.Enum:
		return 0, u.errorAt(pos, len(name), fmt.Sprintf("%s can't extend %s, which is an enum.", s.name, name), "")
	case b.kind == meta.Extern || b.kind == meta.RefCountedExtern || b.extern != nil:
		return 0, u.errorAt(pos, len(name), fmt.Sprintf("%s can't extend %s, which is an extern.", s.name, name),
			"Extend the extern's base class instead.")
	case b.kind != meta.Object && b.kind != meta.RefCounted && b.class == nil:
		return 0, u.errorAt(pos, len(name), fmt.Sprintf("%s can't extend %s, which is not a class.", s.name, name), "")
	}
	kind, err := u.kindOf(b, append(seen, s))
	if err != nil {
		return 0, err
	}
	switch {
	case ext && kind == meta.RefCounted:
		kind = meta.RefCountedExtern
	case ext:
		kind = meta.Extern
	}
	s.kind = kind
	return kind, nil
}

var knownAnnotations = []string{"const", "deferred", "export", "export_category", "export_dir", "export_enum", "export_file", "export_flags",
	"export_group", "export_multiline", "export_placeholder", "export_range", "export_storage", "export_subgroup", "icon", "onready",
	"override", "rpc", "static", "tool", "virtual"}

// sectionAnnotations start an inspector section at their var, which holds it and the vars after it.
var sectionAnnotations = []string{"export_category", "export_group", "export_subgroup"}

// annotations checks the annotations of a declaration of the given kind, e.g. "func", and returns them by name.
func (u *unit) annotations(list []*Annotation, kind string, allowed ...string) (map[string]*Annotation, error) {
	found := map[string]*Annotation{}
	for _, a := range list {
		switch {
		case !slices.Contains(knownAnnotations, a.Name):
			hint := "Known annotations: @" + strings.Join(knownAnnotations, ", @") + "."
			if s := suggest(a.Name, knownAnnotations...); s != "" {
				hint = fmt.Sprintf("Did you mean \"@%s\"?", s)
			}
			return nil, u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Unknown annotation @%s.", a.Name), hint)
		case a.Name == "deferred" && !slices.Contains(allowed, a.Name):
			return nil, u.errorAt(a.Pos, len(a.Name)+1, "Annotation @deferred can only be used in externs.",
				"It makes calls on an extern go through call_deferred.")
		case !slices.Contains(allowed, a.Name):
			return nil, u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotation @%s can't be used on %s.", a.Name, kind), "")
		case found[a.Name] != nil:
			return nil, u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotation @%s is used twice.", a.Name), "")
		case len(a.Args) > 0 && !slices.Contains([]string{"export_category", "export_enum", "export_file", "export_flags", "export_group",
			"export_placeholder", "export_range", "export_subgroup", "icon", "rpc"}, a.Name):
			return nil, u.errorAt(a.Args[0].Pos, len(a.Args[0].Value), fmt.Sprintf("Annotation @%s takes no arguments.", a.Name), "")
		}
		found[a.Name] = a
	}
	for _, pair := range [][2]string{{"static", "virtual"}, {"static", "override"}, {"static", "const"}, {"virtual", "override"},
		{"static", "rpc"}, {"virtual", "rpc"}, {"override", "rpc"}} {
		if a := found[pair[1]]; a != nil && found[pair[0]] != nil {
			return nil, u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotations @%s and @%s can't be used together.", pair[0], pair[1]), "")
		}
	}
	return found, nil
}

func (u *unit) buildFunc(f *Func, ext bool) (*funcModel, error) {
	allowed := []string{"const", "override", "rpc", "static", "virtual"}
	if ext {
		allowed = []string{"const", "deferred", "rpc"}
	}
	a, err := u.annotations(f.Annotations, "a func", allowed...)
	if err != nil {
		return nil, err
	}
	m := &funcModel{f: f, virtual: a["virtual"] != nil, override: a["override"] != nil, isConst: a["const"] != nil,
		static: a["static"] != nil, deferred: a["deferred"] != nil}
	if rpc := a["rpc"]; rpc != nil {
		if m.rpc, err = u.rpcConfig(rpc, ext); err != nil {
			return nil, err
		}
	}
	for i, p := range f.Params {
		t, err := u.resolve(p.Type, false)
		if err != nil {
			return nil, err
		}
		m.params = append(m.params, t)
		switch {
		case ext && p.Default != nil:
			return nil, u.errorAt(p.Default.Pos, 1, "Extern functions can't have default values.", "Externs only declare what another package defines.")
		case p.Default == nil && i > 0 && f.Params[i-1].Default != nil:
			return nil, u.errorAt(p.Pos, len(p.Name), fmt.Sprintf("Parameter %s needs a default value, since a parameter before it has one.", p.Name),
				"Parameters with default values must come last.")
		}
	}
	if m.ret, err = u.resolve(f.Return, true); err != nil {
		return nil, err
	}
	switch {
	case ext && f.Body != nil:
		return nil, u.errorAt(f.Body.Pos, 1, "Extern functions can't have a body.", "Externs only declare what another package defines.")
	case m.deferred && !m.ret.void:
		return nil, u.errorAt(f.Pos, 4, fmt.Sprintf("The @deferred function %s must return void.", f.Name), "Deferred calls run later, so they can't return a value.")
	}
	return m, nil
}

// rpcValues are the strings @rpc takes, as in GDScript: each sets one setting to a C++ value.
var rpcValues = map[string][2]string{
	"authority":          {"mode", "MultiplayerAPI::RPC_MODE_AUTHORITY"},
	"any_peer":           {"mode", "MultiplayerAPI::RPC_MODE_ANY_PEER"},
	"call_remote":        {"sync", "false"},
	"call_local":         {"sync", "true"},
	"unreliable":         {"transfer mode", "MultiplayerPeer::TRANSFER_MODE_UNRELIABLE"},
	"unreliable_ordered": {"transfer mode", "MultiplayerPeer::TRANSFER_MODE_UNRELIABLE_ORDERED"},
	"reliable":           {"transfer mode", "MultiplayerPeer::TRANSFER_MODE_RELIABLE"},
}

// rpcConfig returns the configuration from the @rpc annotation a.
func (u *unit) rpcConfig(a *Annotation, ext bool) (*rpcModel, error) {
	if ext && len(a.Args) > 0 {
		return nil, u.errorAt(a.Args[0].Pos, len(a.Args[0].Value), "In externs, @rpc takes no arguments: the class that defines the function configures it.", "")
	}
	set := map[string]string{"mode": "MultiplayerAPI::RPC_MODE_AUTHORITY", "sync": "false",
		"transfer mode": "MultiplayerPeer::TRANSFER_MODE_UNRELIABLE", "channel": "0"}
	seen := map[string]bool{}
	for i, arg := range a.Args {
		name, _ := strconv.Unquote(arg.Value)
		setting, known := rpcValues[name]
		_, notInt := strconv.ParseInt(arg.Value, 0, 64)
		switch {
		case notInt == nil && i < len(a.Args)-1:
			return nil, u.errorAt(arg.Pos, len(arg.Value), "The channel must be the last argument of @rpc.", "")
		case notInt == nil:
			setting = [2]string{"channel", arg.Value}
		case !known && rpcValues[arg.Value][0] != "":
			return nil, u.errorAt(arg.Pos, len(arg.Value), "The arguments of @rpc are strings.", fmt.Sprintf("Write %q.", arg.Value))
		case !known:
			var names []string
			for n := range rpcValues {
				names = append(names, n)
			}
			slices.Sort(names)
			hint := `Known values: "` + strings.Join(names, `", "`) + `", and a channel number.`
			if s := suggest(strings.Trim(arg.Value, `"'`), names...); s != "" {
				hint = fmt.Sprintf("Did you mean %q?", s)
			}
			return nil, u.errorAt(arg.Pos, len(arg.Value), fmt.Sprintf("Unknown @rpc argument %s.", arg.Value), hint)
		}
		if seen[setting[0]] {
			return nil, u.errorAt(arg.Pos, len(arg.Value), fmt.Sprintf("Annotation @rpc sets the %s twice.", setting[0]), "")
		}
		seen[setting[0]] = true
		set[setting[0]] = setting[1]
	}
	return &rpcModel{mode: set["mode"], transfer: set["transfer mode"], callLocal: set["sync"] == "true", channel: set["channel"]}, nil
}

func (u *unit) buildSignal(s *Signal) (*signalModel, error) {
	if _, err := u.annotations(s.Annotations, "a signal"); err != nil {
		return nil, err
	}
	m := &signalModel{s: s}
	for _, p := range s.Params {
		t, err := u.resolve(p.Type, false)
		if err != nil {
			return nil, err
		}
		if p.Default != nil {
			return nil, u.errorAt(p.Default.Pos, 1, "Signal parameters can't have default values.", "")
		}
		m.params = append(m.params, t)
	}
	return m, nil
}

func (u *unit) buildVar(v *Var, ext bool) (*varModel, error) {
	allowed := append([]string{"onready", "export", "export_dir", "export_enum", "export_file", "export_flags",
		"export_multiline", "export_placeholder", "export_range", "export_storage"}, sectionAnnotations...)
	if ext {
		allowed = nil
	}
	a, err := u.annotations(v.Annotations, "a var", allowed...)
	if err != nil {
		return nil, err
	}
	m := &varModel{v: v, onready: a["onready"] != nil, usage: "PROPERTY_USAGE_NONE", hint: "PROPERTY_HINT_NONE"}
	if m.t, err = u.resolve(v.Type, false); err != nil {
		return nil, err
	}
	if ext && (v.Init != nil || v.Property != nil) {
		return nil, u.errorAt(v.Pos, 3, "Extern variables can't have an initial value or a property body.", "Externs only declare what another package defines.")
	}
	if err := u.exportHint(m); err != nil {
		return nil, err
	}
	if err := u.sections(m); err != nil {
		return nil, err
	}
	if v.Property == nil {
		m.getter, m.setter = "get_"+v.Name, "set_"+v.Name
		return m, nil
	}
	for _, acc := range v.Property.Accessors {
		switch {
		case acc.Decl != nil:
			m.decls = append(m.decls, acc.Decl)
		case acc.Get != nil && m.get != nil, acc.Set != nil && m.set != nil:
			return nil, u.errorAt(acc.Pos, 3, fmt.Sprintf("Property %s has two %s blocks.", v.Name, map[bool]string{true: "get", false: "set"}[acc.Get != nil]), "")
		case acc.Get != nil:
			m.get, m.getter = acc.Get, "get_"+v.Name
		default:
			m.set, m.setter = acc.Set, "set_"+v.Name
			if d := acc.Set.Param.Default; d != nil {
				return nil, u.errorAt(d.Pos, 1, "The setter's parameter can't have a default value.", "")
			}
			if t := acc.Set.Param.Type; t != nil && (v.Type == nil || typeString(t) != typeString(v.Type)) {
				return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("The setter's parameter must have the property's type, %s.", typeString(v.Type)),
					"Leave the type out: \"set(value) { ... }\".")
			}
		}
	}
	if m.get == nil {
		return nil, u.errorAt(v.Pos, 3, fmt.Sprintf("Property %s has no get block.", v.Name), "Godot doesn't support set-only properties.")
	}
	return m, nil
}

// typeString spells t as in GD++.
func typeString(t *Type) string {
	if t == nil {
		return "Variant"
	}
	if len(t.Args) == 0 {
		return t.Name
	}
	var args []string
	for _, a := range t.Args {
		args = append(args, typeString(a))
	}
	return t.Name + "[" + strings.Join(args, ", ") + "]"
}

// exportHint sets the property usage and hint of m from its export annotations.
func (u *unit) exportHint(m *varModel) error {
	var export *Annotation
	for _, ann := range m.v.Annotations { // In source order, so the second one is reported.
		if strings.HasPrefix(ann.Name, "export") && !slices.Contains(sectionAnnotations, ann.Name) {
			if export != nil {
				return u.errorAt(ann.Pos, len(ann.Name)+1, "A variable can have only one export annotation.", "")
			}
			export = ann
		}
	}
	if export == nil {
		return nil
	}
	m.usage = "PROPERTY_USAGE_DEFAULT"
	var args []string
	for _, arg := range export.Args {
		s, err := u.argValue(arg)
		if err != nil {
			return err
		}
		args = append(args, s)
	}
	isNumber := m.t.cpp == "int64_t" || m.t.cpp == "double"
	switch export.Name {
	case "export":
		if m.t.enum != nil {
			var names []string
			for _, v := range m.t.enum.values {
				names = append(names, fmt.Sprintf("%s:%d", capitalize(v.Name), v.Value))
			}
			m.hint, m.hintString = "PROPERTY_HINT_ENUM", strings.Join(names, ",")
		}
	case "export_storage":
		m.usage = "PROPERTY_USAGE_STORAGE"
	case "export_range":
		if !isNumber || len(args) < 2 {
			return u.errorAt(export.Pos, len(export.Name)+1, "Annotation @export_range needs an int or float variable, and at least a minimum and a maximum.",
				"E.g. \"@export_range(0, 100, 1, \\\"or_greater\\\") var hp: int\".")
		}
		m.hint, m.hintString = "PROPERTY_HINT_RANGE", strings.Join(args, ",")
	case "export_enum", "export_flags":
		if len(args) == 0 || m.t.cpp != "int64_t" && (export.Name == "export_flags" || m.t.cpp != "String") {
			return u.errorAt(export.Pos, len(export.Name)+1, fmt.Sprintf("Annotation @%s needs an int variable and at least one name.", export.Name),
				fmt.Sprintf("E.g. \"@%s(\\\"A\\\", \\\"B\\\") var x: int\".", export.Name))
		}
		m.hint, m.hintString = map[string]string{"export_enum": "PROPERTY_HINT_ENUM", "export_flags": "PROPERTY_HINT_FLAGS"}[export.Name], strings.Join(args, ",")
	case "export_file", "export_dir", "export_multiline", "export_placeholder":
		if m.t.cpp != "String" {
			return u.errorAt(export.Pos, len(export.Name)+1, fmt.Sprintf("Annotation @%s needs a String variable.", export.Name), "")
		}
		m.hint = map[string]string{"export_file": "PROPERTY_HINT_FILE", "export_dir": "PROPERTY_HINT_DIR",
			"export_multiline": "PROPERTY_HINT_MULTILINE_TEXT", "export_placeholder": "PROPERTY_HINT_PLACEHOLDER_TEXT"}[export.Name]
		m.hintString = strings.Join(args, ",")
	}
	return nil
}

// sections sets the inspector sections m starts, from its section annotations.
func (u *unit) sections(m *varModel) error {
	for _, ann := range m.v.Annotations { // In source order, which is the order of the sections.
		if !slices.Contains(sectionAnnotations, ann.Name) {
			continue
		}
		var args []string
		for _, arg := range ann.Args {
			if !isString(arg.Value) {
				args = nil
				break
			}
			value, err := u.argValue(arg)
			if err != nil {
				return err
			}
			args = append(args, value)
		}
		switch {
		case ann.Name == "export_category" && (len(args) != 1 || args[0] == ""):
			return u.errorAt(ann.Pos, len(ann.Name)+1, "Annotation @export_category needs a name, as a string.",
				"E.g. \"@export_category(\\\"Stats\\\") var hp: int\".")
		case len(args) == 0 || len(args) > 2:
			return u.errorAt(ann.Pos, len(ann.Name)+1, fmt.Sprintf("Annotation @%s needs a name and an optional prefix, as strings.", ann.Name),
				fmt.Sprintf("E.g. \"@%s(\\\"Stats\\\") var hp: int\".", ann.Name))
		}
		m.sections = append(m.sections, section{ann, args[0], strings.Join(args[1:], "")})
	}
	return nil
}

// isString reports whether an annotation argument is a string literal.
func isString(arg string) bool { return strings.HasPrefix(arg, "\"") || strings.HasPrefix(arg, "'") }

// argValue returns an annotation argument's value: strings unquoted, other arguments as written.
func (u *unit) argValue(arg *Arg) (string, error) {
	if !isString(arg.Value) {
		return arg.Value, nil
	}
	s, err := strconv.Unquote("\"" + arg.Value[1:len(arg.Value)-1] + "\"")
	if err != nil {
		return "", u.errorAt(arg.Pos, len(arg.Value), "This string has an invalid escape sequence.", "")
	}
	return s, nil
}

// capitalize turns an enum value name into Godot's editor spelling, e.g. "SOME_VALUE" into "Some Value".
func capitalize(s string) string {
	words := strings.Fields(strings.ReplaceAll(s, "_", " "))
	for i, w := range words {
		words[i] = strings.ToUpper(w[:1]) + strings.ToLower(w[1:])
	}
	return strings.Join(words, " ")
}

func (u *unit) buildExterns() error {
	externs := u.file.InlineExterns
	if u.file.FileExtern != nil {
		externs = append([]*Extern{u.file.FileExtern}, externs...)
	}
	for _, e := range externs {
		s := u.symbols[e.Name]
		m := &externModel{name: e.Name, ext: e, base: baseName(e.Extends), refCounted: s.kind == meta.RefCountedExtern}
		if _, err := u.annotations(e.Annotations, "an extern"); err != nil {
			return err
		}
		names := map[string]bool{}
		for _, member := range e.Members {
			var err error
			switch {
			case member.Func != nil:
				var f *funcModel
				if f, err = u.buildFunc(member.Func, true); err == nil {
					m.funcs = append(m.funcs, f)
					err = u.unique(names, f.f.Pos, "func", f.f.Name)
				}
			case member.Var != nil:
				var v *varModel
				if v, err = u.buildVar(member.Var, true); err == nil {
					m.vars = append(m.vars, v)
					err = u.unique(names, v.v.Pos, "var", v.v.Name, v.getter, v.setter)
				}
			case member.Signal != nil:
				var sig *signalModel
				if sig, err = u.buildSignal(member.Signal); err == nil {
					m.signals = append(m.signals, sig)
					err = u.unique(names, sig.s.Pos, "signal", sig.s.Name)
				}
			default:
				keyword, pos := member.keyword()
				return u.errorAt(pos, len(keyword), fmt.Sprintf("Externs can't contain %s.", map[string]string{"decl": "decl or impl blocks",
					"ctor": "a ctor", "dtor": "a dtor", "enum": "enums", "import": "imports", "noimport": "imports"}[keyword]),
					"Externs only declare the funcs, vars and signals that another package defines.")
			}
			if err != nil {
				return err
			}
		}
		u.externs = append(u.externs, m)
	}
	return nil
}

// unique records the names a member declares, and fails if one is taken.
func (u *unit) unique(names map[string]bool, pos lexer.Position, keyword string, declared ...string) error {
	for _, name := range declared {
		if name == "" {
			continue
		}
		if names[name] {
			return u.errorAt(pos, len(keyword), fmt.Sprintf("The name %q is already used by another member.", name),
				"Members share one namespace. A var x also declares get_x and set_x, and a signal declares its emit function.")
		}
		names[name] = true
	}
	return nil
}

func (u *unit) buildClasses() error {
	var ordered []*Class
	var visit func(c *Class)
	visit = func(c *Class) {
		if slices.Contains(ordered, c) {
			return
		}
		if b := u.symbols[baseName(c.Extends)]; b != nil && b.class != nil {
			visit(b.class)
		}
		ordered = append(ordered, c)
	}
	if u.file.FileClass != nil {
		visit(u.file.FileClass)
	}
	for _, c := range u.file.InlineClasses {
		visit(c)
	}
	for _, c := range ordered {
		m, err := u.buildClass(c, c == u.file.FileClass)
		if err != nil {
			return err
		}
		u.classes = append(u.classes, m)
	}
	return nil
}

func (u *unit) buildClass(c *Class, fileLevel bool) (*classModel, error) {
	m := &classModel{name: c.Name, cls: c, base: baseName(c.Extends), refCounted: u.symbols[c.Name].kind == meta.RefCounted}
	a, err := u.annotations(c.Annotations, "a class", "icon", "tool")
	if err != nil {
		return nil, err
	}
	m.tool = a["tool"] != nil
	if _, err := u.classIcon(c); err != nil {
		return nil, err
	}
	names := map[string]bool{}
	var declared []*symbol // Enums declared in the class.
	for _, member := range c.Members {
		var err error
		switch {
		case member.Code != nil:
			m.codes = append(m.codes, member.Code)
		case member.Ctor != nil && m.ctor != nil, member.Dtor != nil && m.dtor != nil:
			keyword, pos := member.keyword()
			return nil, u.errorAt(pos, len(keyword), fmt.Sprintf("Class %s has two %ss.", c.Name, keyword), "")
		case member.Ctor != nil:
			m.ctor = member.Ctor
		case member.Dtor != nil:
			m.dtor = member.Dtor
		case member.Func != nil:
			var f *funcModel
			if f, err = u.buildFunc(member.Func, false); err == nil {
				m.funcs = append(m.funcs, f)
				err = u.unique(names, f.f.Pos, "func", f.f.Name)
			}
		case member.Var != nil:
			var v *varModel
			if v, err = u.buildVar(member.Var, false); err == nil {
				m.vars = append(m.vars, v)
				err = u.unique(names, v.v.Pos, "var", v.v.Name, v.getter, v.setter)
				for _, s := range v.sections {
					if err != nil || s.ann.Name != "export_category" {
						continue
					}
					if names[s.name] {
						err = u.errorAt(s.ann.Pos, len(s.ann.Name)+1, fmt.Sprintf("The category name %q is already used by another category or member.", s.name),
							"Godot adds a category as a property named after it.")
					}
					names[s.name] = true
				}
			}
		case member.Signal != nil:
			var sig *signalModel
			if sig, err = u.buildSignal(member.Signal); err == nil {
				m.signals = append(m.signals, sig)
				err = u.unique(names, sig.s.Pos, "signal", sig.s.Name)
			}
		case member.Enum != nil && member.Enum.Value != nil:
			if _, err = u.annotations(member.Enum.Annotations, "a constant"); err == nil {
				m.consts = append(m.consts, member.Enum)
				err = u.unique(names, member.Enum.Pos, "enum", member.Enum.Name)
			}
		case member.Enum != nil:
			_, err = u.annotations(member.Enum.Annotations, "an enum")
			declared = append(declared, u.symbols[member.Enum.Name])
		case member.Import != nil:
			m.imports = append(m.imports, member.Import)
		default:
			m.noimports = append(m.noimports, member.NoImport)
		}
		if err != nil {
			return nil, err
		}
	}
	// @onready initializers run at the start of _ready, so declare it if the class doesn't.
	if slices.ContainsFunc(m.vars, func(v *varModel) bool { return v.v.Init != nil && v.onready }) &&
		!slices.ContainsFunc(m.funcs, func(f *funcModel) bool { return f.override && f.f.Name == "_ready" }) {
		m.funcs = append(m.funcs, &funcModel{f: &Func{Name: "_ready"}, ret: &gtype{cpp: "void", doc: "void", void: true}, override: true})
	}
	// The enums the class exposes: those in its API, those it imports, and those declared in it (or, for the
	// file-level class, in its file).
	var api []*gtype
	for _, f := range m.funcs {
		if !f.override {
			api = append(append(api, f.ret), f.params...)
		}
	}
	for _, v := range m.vars {
		api = append(api, v.t)
	}
	for _, s := range m.signals {
		api = append(api, s.params...)
	}
	var exposed []*symbol
	for _, t := range api {
		if t.enum != nil && !slices.Contains(exposed, t.enum) {
			exposed = append(exposed, t.enum)
		}
	}
	for _, list := range [][]*Type{m.imports, m.noimports} {
		for _, t := range list {
			if _, builtin := builtins[t.Name]; len(t.Args) > 0 || builtin {
				return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Built-in type %s can't be imported.", typeString(t)), "Only classes, externs and enums have includes.")
			}
			if u.symbols[t.Name] == nil {
				return nil, u.unknownType(t)
			}
		}
	}
	for _, t := range m.imports {
		if s := u.symbols[t.Name]; s.kind == meta.Enum && !slices.Contains(exposed, s) {
			exposed = append(exposed, s)
		}
	}
	if fileLevel {
		declared = u.enums
	}
	for _, s := range declared {
		if !slices.Contains(exposed, s) {
			exposed = append(exposed, s)
		}
	}
	for _, t := range m.noimports {
		s := u.symbols[t.Name]
		if s.kind != meta.Enum {
			continue
		}
		if slices.ContainsFunc(api, func(g *gtype) bool { return g.enum == s }) {
			return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Class %s uses enum %s in its API, so it must export it.", c.Name, t.Name), "Remove this noimport.")
		}
		exposed = slices.DeleteFunc(exposed, func(e *symbol) bool { return e == s })
	}
	m.enums = exposed
	return m, nil
}
