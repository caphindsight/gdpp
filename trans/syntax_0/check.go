package syntax_0

import (
	"fmt"
	"regexp"
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
	classes []*classModel      // Bases first.
	externs []*externModel
}

type classModel struct {
	name       string
	cls        *Class
	base       string
	refCounted bool
	gameOnly   bool    // Whether @game_only guards all its code against running in the editor.
	trace      bool    // Whether its @trace is on: it traces its lifetime, signals, and all its funcs and vars.
	profile    bool    // Whether its @profile is on: it profiles all its funcs, and the get and set blocks of its vars.
	codes      []*Code // decl and impl blocks inside the class.
	globals    []*Code // @global decl and impl blocks, outside the class and namespace godot.
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
	f                                  *Func
	params                             []*gtype
	ret                                *gtype
	virtual, override, isConst, static bool
	deferral                           string    // "deferred" or "thread_safe" with that annotation, else empty.
	hidden                             bool      // The generated body of a class's @deferred or @thread_safe func.
	trace, profile                     bool      // Whether its @trace or @profile, or its class's, is on.
	rpc                                *rpcModel // Nil without @rpc.
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
	trace            bool   // Whether its @trace, or its class's, is on: the class's funcs print its changes.
	profile          bool   // Whether its @profile, or its class's, is on: it profiles its getter and setter.
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
	trace  bool // Whether its @trace, or its class's, is on: emitting it prints it.
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
		s.name, s.include, s.gdpp = name, `"`+name+`.h"`, true
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
			continue // A class constant.
		}
		a, err := u.annotations(e.Annotations, "an enum", "bitfield")
		if err != nil {
			return nil, err
		}
		s := &symbol{kind: meta.Enum, enum: e, bitfield: a["bitfield"] != nil}
		if e.Extends != nil {
			s.base = e.Extends.Name
		}
		if err := declare(e.Pos, e.Name, s); err != nil {
			return nil, err
		}
		flags, afterFlags := false, false // Whether a value without "=" came yet, and a value with "=" after it.
		for _, entry := range e.Entries {
			if entry.Name == "extends" {
				return nil, u.errorAt(entry.Pos, len(entry.Name), "Expected an enum value name, but found keyword \"extends\".",
					"\"extends\" comes before the values and names the base enum: \"enum Name { extends Base A B }\".")
			}
			if slices.ContainsFunc(s.values, func(v meta.EnumValue) bool { return v.Name == entry.Name }) {
				return nil, u.errorAt(entry.Pos, len(entry.Name), fmt.Sprintf("Enum %s has two values named %s.", e.Name, entry.Name), "")
			}
			if s.bitfield && entry.Value == nil && afterFlags {
				return nil, u.errorAt(entry.Pos, len(entry.Name), fmt.Sprintf("Bitfield %s's values without \"=\" must be next to each other.", e.Name),
					"Set this value with \"=\", or move it next to the others.")
			}
			flags, afterFlags = flags || entry.Value == nil, flags && entry.Value != nil
			doc := ""
			if entry.Doc != nil {
				doc = entry.Doc.Text
			}
			v := meta.EnumValue{Name: entry.Name, Doc: doc, Implicit: entry.Value == nil}
			switch {
			case entry.Value == nil:
			case entry.Value.Int != nil:
				v.Value = entry.Value.Int.Value
			default:
				v.Expr = entry.Value.String()
			}
			s.values = append(s.values, v)
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
			c := s.class
			decls = append(decls, meta.Declaration{Name: s.name, Kind: meta.ClassDecl, Base: baseName(c.Extends), Icon: icon,
				Tool: hasAnnotation(c, "tool"), GameOnly: hasAnnotation(c, "game_only"), Trace: hasAnnotation(c, "trace"), Profile: hasAnnotation(c, "profile")})
		case s.extern != nil:
			decls = append(decls, meta.Declaration{Name: s.name, Kind: meta.ExternDecl, Base: baseName(s.extern.Extends)})
		default:
			decls = append(decls, meta.Declaration{Name: s.name, Kind: meta.EnumDecl, Base: s.base, Values: s.values, Bitfield: s.bitfield})
		}
	}
	return decls, nil
}

// hasAnnotation reports whether class c has the annotation named name.
func hasAnnotation(c *Class, name string) bool {
	return slices.ContainsFunc(c.Annotations, func(a *Annotation) bool { return a.Name == name })
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
		if s.local() {
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
	if opts.SourceName == "" {
		opts.SourceName = filename
	}
	u.opts = opts
	for _, d := range opts.Dependencies {
		if s := u.symbols[d.Name]; s != nil {
			if !s.local() {
				continue // A duplicate dependency.
			}
			return nil, u.errorAt(s.pos(), 0, fmt.Sprintf("The name %q is already declared by a dependency.", d.Name),
				"Names must differ from Godot's, and from those of the package's other classes, externs and enums.")
		}
		s := &symbol{name: d.Name, kind: d.Kind, include: d.Include, values: d.Values, base: d.Base, gdpp: d.Gdpp, bitfield: d.Bitfield}
		if d.Kind == meta.GodotEnum {
			s.values, s.godotNames = godotValues(d.Name, d.Values)
		}
		u.symbols[d.Name] = s
	}
	for _, s := range u.sortedSymbols() {
		if s.kind == 0 {
			if _, err := u.kindOf(s, nil); err != nil {
				return nil, err
			}
		}
		if _, err := u.enumValues(s, nil); err != nil {
			return nil, err
		}
	}
	for _, s := range u.symbols {
		u.enumValues(s, nil) // Dependencies, which classes may expose.
	}
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
	what, extends := "Class", (*Type)(nil)
	if ext {
		what, extends = "Extern", s.extern.Extends
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
		return 0, u.errorAt(pos, len(name), fmt.Sprintf("%s %s extends itself through its bases.", what, s.name), "")
	case b == nil:
		t := &Type{Pos: pos, Name: name}
		if extends == nil {
			return 0, u.errorAt(pos, 0, fmt.Sprintf("Unknown base class \"RefCounted\" of %s.", s.name), "Add RefCounted to the dependencies, or extend another class.")
		}
		return 0, u.unknownName(t, "base class")
	case b.kind == meta.Enum:
		return 0, u.errorAt(pos, len(name), fmt.Sprintf("%s can't extend %s, which is an enum.", s.name, name), "")
	case !ext && b.isExtern():
		return 0, u.errorAt(pos, len(name), fmt.Sprintf("%s can't extend %s, which is an extern.", s.name, name),
			"Extend the extern's base class instead.")
	case b.kind != meta.Object && b.kind != meta.RefCounted && b.class == nil && !b.isExtern():
		return 0, u.errorAt(pos, len(name), fmt.Sprintf("%s can't extend %s, which is not a class.", s.name, name), "")
	}
	kind, err := u.kindOf(b, append(seen, s))
	if err != nil {
		return 0, err
	}
	switch {
	case ext && (kind == meta.RefCounted || kind == meta.RefCountedExtern):
		kind = meta.RefCountedExtern
	case ext:
		kind = meta.Extern
	}
	s.kind = kind
	return kind, nil
}

// enumValues returns the values of enum s, those of its base first, with expressions computed, and stores them
// in s. Values without "=" count on from the previous one, e.g. the base's last one. In a bitfield, they are the
// flags instead: powers of two, from the smallest one above the base's values.
// Only the file's own enums report errors: a dependency's are reported when its own file is transpiled, so there
// a broken base counts as empty, and a broken expression as a value without "=".
func (u *unit) enumValues(s *symbol, seen []*symbol) ([]meta.EnumValue, error) {
	if s.kind != meta.Enum && s.kind != meta.GodotEnum || s.base == "" && !slices.ContainsFunc(s.values, func(v meta.EnumValue) bool { return v.Implicit || v.Expr != "" }) {
		return s.values, nil
	}
	seen = append(seen, s)
	// enum returns the values of the enum called name, which s names at pos.
	enum := func(name string, pos lexer.Position) ([]meta.EnumValue, error) {
		b := u.symbols[name]
		switch {
		case b == nil:
			var names []string
			for n, sym := range u.symbols {
				if sym.kind == meta.Enum || sym.kind == meta.GodotEnum {
					names = append(names, n)
				}
			}
			hint := "Enums are the package's enums, or engine enums, e.g. Node.ProcessMode."
			if sug := suggest(name, names...); sug != "" {
				hint = fmt.Sprintf("Did you mean %q?", sug)
			}
			return nil, u.errorAt(pos, len(name), fmt.Sprintf("Unknown enum %q.", name), hint)
		case b.kind != meta.Enum && b.kind != meta.GodotEnum:
			return nil, u.errorAt(pos, len(name), fmt.Sprintf("%s is not an enum.", name), "")
		case slices.Contains(seen, b):
			return nil, u.errorAt(pos, len(name), fmt.Sprintf("Enum %s depends on itself, through its base or its values.", s.name), "")
		}
		return u.enumValues(b, seen)
	}
	var values []meta.EnumValue
	if s.base != "" {
		var pos lexer.Position
		if s.local() {
			pos = s.enum.Extends.Pos
		}
		base, err := enum(s.base, pos)
		if err != nil && s.local() {
			return nil, err
		}
		if b := u.symbols[s.base]; err == nil && s.local() && b.bitfield != s.bitfield {
			if s.bitfield {
				return nil, u.errorAt(pos, len(s.base), fmt.Sprintf("Bitfield %s can't extend %s, which is not a bitfield.", s.name, s.base), "")
			}
			return nil, u.errorAt(pos, len(s.base), fmt.Sprintf("%s can't extend %s, which is a bitfield.", s.name, s.base),
				fmt.Sprintf("Add @bitfield to %s.", s.name))
		}
		values = slices.Clone(base)
	}
	inherited := len(values)
	next, flag := int64(0), int64(1)
	for _, w := range values {
		next = w.Value + 1
		for flag > 0 && w.Value >= flag {
			flag <<= 1
		}
	}
	for i, v := range s.values {
		var entry *EnumEntry
		var x *EnumExpr
		if s.local() {
			entry, x = s.enum.Entries[i], s.enum.Entries[i].Value
		} else if v.Expr != "" {
			x = parseEnumExpr(v.Expr)
		}
		implicit := v.Implicit || v.Expr != "" && x == nil
		if v.Expr != "" && x != nil {
			value, err := u.enumExpr(s, x, values, enum)
			if err != nil && s.local() {
				return nil, err
			}
			v.Value, implicit = value, err != nil
		}
		switch {
		case implicit && s.bitfield && flag <= 0 && s.local():
			return nil, u.errorAt(entry.Pos, len(entry.Name), fmt.Sprintf("Bitfield %s has no bits left for %s.", s.name, v.Name),
				"The largest flag is 2^62. Set this value with \"=\", or split the bitfield in two.")
		case implicit && s.bitfield:
			v.Value, flag = flag, flag<<1
		case implicit:
			v.Value = next
		}
		next = v.Value + 1
		if k := slices.IndexFunc(values, func(w meta.EnumValue) bool { return w.Name == v.Name }); k >= 0 {
			if !s.local() {
				continue
			}
			hint := ""
			if k < inherited {
				hint = fmt.Sprintf("It inherits %s from %s.", v.Name, s.base)
			}
			return nil, u.errorAt(entry.Pos, len(entry.Name), fmt.Sprintf("Enum %s has two values named %s.", s.name, v.Name), hint)
		}
		values = append(values, meta.EnumValue{Name: v.Name, Value: v.Value, Doc: v.Doc})
	}
	s.values, s.base = values, ""
	return values, nil
}

// enumExpr returns the number of x, an expression in a value of enum s. s's own values are those in values, the
// ones before it; enum returns the values of other enums.
func (u *unit) enumExpr(s *symbol, x *EnumExpr, values []meta.EnumValue,
	enum func(string, lexer.Position) ([]meta.EnumValue, error)) (int64, error) {
	switch {
	case x.Int != nil:
		return x.Int.Value, nil
	case x.Ref != nil:
		return u.enumRef(s, x.Ref, values, enum)
	}
	var left int64
	var err error
	if x.Left != nil {
		if left, err = u.enumExpr(s, x.Left, values, enum); err != nil {
			return 0, err
		}
	}
	right, err := u.enumExpr(s, x.Right, values, enum)
	switch {
	case err != nil:
		return 0, err
	case right == 0 && (x.Op == "/" || x.Op == "%"):
		return 0, u.errorAt(x.Right.Pos, 1, "Division by zero.", "")
	}
	switch x.Op { // Like int64 in GDScript, results wrap around. For a unary "-", left is 0.
	case "~":
		return ^right, nil
	case "|":
		return left | right, nil
	case "^":
		return left ^ right, nil
	case "&":
		return left & right, nil
	case "+":
		return left + right, nil
	case "-":
		return left - right, nil
	case "*":
		return left * right, nil
	case "/":
		return left / right, nil
	}
	return left % right, nil
}

// enumRef returns the number of ref, in a value of enum s: a value of s, e.g. HEARTS, a value of an enum in GD++,
// e.g. Suit.HEARTS or GeometryInstance3D.ShadowCastingSetting.ON, or an engine enum's value as a constant of its
// class, like in GDScript, e.g. GeometryInstance3D.SHADOW_CASTING_SETTING_ON. s's own values are those in values,
// the ones before it; enum returns the values of other enums.
func (u *unit) enumRef(s *symbol, ref *EnumRef, values []meta.EnumValue,
	enum func(string, lexer.Position) ([]meta.EnumValue, error)) (int64, error) {
	i := strings.LastIndex(ref.Name, ".")
	enumName, name := s.name, ref.Name[i+1:]
	if i >= 0 {
		enumName = ref.Name[:i]
	}
	pos := ref.Pos
	namePos := pos
	namePos.Offset, namePos.Column = pos.Offset+i+1, pos.Column+i+1
	if b := u.symbols[enumName]; b != nil && (b.kind == meta.Object || b.kind == meta.RefCounted) {
		return u.classConstant(enumName, name, namePos)
	}
	from := values
	if enumName != s.name {
		var err error
		if from, err = enum(enumName, pos); err != nil {
			return 0, err
		}
	}
	if v, ok := enumValue(from, name); ok {
		return v.Value, nil
	}
	hint := ""
	if enumName == s.name {
		hint = "A value can only be one of the values before it."
	}
	if b := u.symbols[enumName]; b != nil {
		if k := slices.Index(b.godotNames, name); k >= 0 {
			class := enumName[:strings.LastIndex(enumName, ".")+1]
			return 0, u.errorAt(namePos, len(name), fmt.Sprintf("Enum %s has no value %s.", enumName, name),
				fmt.Sprintf("Write %s.%s, or %s%s as in GDScript.", enumName, b.values[k].Name, class, name))
		}
	}
	return 0, u.noValue(namePos, enumName, from, name, hint)
}

// classConstant returns the number of the value that the engine class called class has as the constant name, e.g.
// GeometryInstance3D's SHADOW_CASTING_SETTING_ON. pos is the position of name.
func (u *unit) classConstant(class, name string, pos lexer.Position) (int64, error) {
	var names []string
	for n, s := range u.symbols {
		if s.kind != meta.GodotEnum || !strings.HasPrefix(n, class+".") {
			continue
		}
		if k := slices.Index(s.godotNames, name); k >= 0 {
			return s.values[k].Value, nil
		}
		names = append(names, s.godotNames...)
	}
	hint := ""
	if sug := suggest(name, names...); sug != "" {
		hint = fmt.Sprintf("Did you mean %q?", sug)
	}
	return 0, u.errorAt(pos, len(name), fmt.Sprintf("%s has no enum value %s.", class, name), hint)
}

// enumValue returns the value called name among values.
func enumValue(values []meta.EnumValue, name string) (meta.EnumValue, bool) {
	if i := slices.IndexFunc(values, func(v meta.EnumValue) bool { return v.Name == name }); i >= 0 {
		return values[i], true
	}
	return meta.EnumValue{}, false
}

// godotValues returns the values of the engine enum called name, with their GD++ names, and Godot's names. A GD++
// name leaves out the prefix that the enum's name gives, e.g. ON for GeometryInstance3D.ShadowCastingSetting's
// SHADOW_CASTING_SETTING_ON, unless the rest isn't a name, e.g. Key's KEY_3, or is another value's name.
func godotValues(name string, values []meta.EnumValue) ([]meta.EnumValue, []string) {
	var godotNames []string
	for _, v := range values {
		godotNames = append(godotNames, v.Name)
	}
	prefix := upperSnake(name[strings.LastIndex(name, ".")+1:]) + "_"
	values = slices.Clone(values)
	for i, v := range values {
		if short, ok := strings.CutPrefix(v.Name, prefix); ok && identRegexp.MatchString(short) && !slices.Contains(godotNames, short) {
			values[i].Name = short
		}
	}
	return values, godotNames
}

// noValue returns the error for name at pos, which isn't among values, those of the enum called enumName.
func (u *unit) noValue(pos lexer.Position, enumName string, values []meta.EnumValue, name, hint string) error {
	var names []string
	for _, v := range values {
		names = append(names, v.Name)
	}
	if sug := suggest(name, names...); sug != "" {
		hint = fmt.Sprintf("Did you mean %q?", sug)
	}
	return u.errorAt(pos, len(name), fmt.Sprintf("Enum %s has no value %s.", enumName, name), hint)
}

// enumShorthand turns init, the initial or default value of a value of type t, into C++ if it's a value of t's
// enum, e.g. ON or Suit.ON, or for a bitfield, values combined with |, &, ^, ~ and parentheses, e.g. RED | BOLD.
// Other values are C++ already.
func (u *unit) enumShorthand(t *gtype, init *Init) error {
	s := t.enum
	if s == nil || init == nil || init.Block != nil {
		return nil
	}
	l, err := gdppLexer.LexString("", init.Expr)
	if err != nil {
		return nil
	}
	tokens, err := lexer.ConsumeAll(l)
	if err != nil {
		return nil
	}
	var cpp strings.Builder
	for i := 0; i < len(tokens); i++ {
		name := tokens[i]
		qualified := name.Value == s.name && isPunct(at(tokens, i+1), ".") && at(tokens, i+2).Type == tokIdent
		if qualified {
			name, i = tokens[i+2], i+2
		}
		v, ok := enumValue(s.values, name.Value)
		switch {
		case name.EOF() || name.Type == tokWhitespace || name.Type == tokNewline || s.bitfield && name.Type == tokPunct && strings.Contains("|&^~()", name.Value):
			cpp.WriteString(name.Value)
		case name.Type == tokIdent && ok:
			cpp.WriteString(s.name + "::" + v.Name)
		case qualified:
			pos := init.Pos
			pos.Offset, pos.Column = pos.Offset+name.Pos.Offset, pos.Column+name.Pos.Offset
			return u.noValue(pos, s.name, s.values, name.Value, "")
		default:
			return nil // C++, e.g. a constant.
		}
	}
	init.Expr = cpp.String()
	return nil
}

var identRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var knownAnnotations = []string{"bitfield", "const", "deferred", "export", "export_category", "export_dir", "export_enum", "export_file", "export_flags",
	"export_group", "export_multiline", "export_placeholder", "export_range", "export_storage", "export_subgroup", "game_only", "global", "icon", "onready",
	"override", "profile", "rpc", "static", "thread_safe", "tool", "trace", "virtual"}

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
		case !slices.Contains(allowed, a.Name):
			return nil, u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotation @%s can't be used on %s.", a.Name, kind), "")
		case found[a.Name] != nil:
			return nil, u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotation @%s is used twice.", a.Name), "")
		case len(a.Args) > 0 && !slices.Contains([]string{"export_category", "export_enum", "export_file", "export_flags", "export_group",
			"export_placeholder", "export_range", "export_subgroup", "icon", "profile", "rpc", "trace"}, a.Name):
			return nil, u.errorAt(a.Args[0].Pos, len(a.Args[0].Value), fmt.Sprintf("Annotation @%s takes no arguments.", a.Name), "")
		}
		found[a.Name] = a
	}
	for _, pair := range [][2]string{{"static", "virtual"}, {"static", "override"}, {"static", "const"}, {"virtual", "override"},
		{"static", "rpc"}, {"virtual", "rpc"}, {"override", "rpc"}, {"static", "deferred"}, {"virtual", "deferred"},
		{"override", "deferred"}, {"static", "thread_safe"}, {"virtual", "thread_safe"}, {"override", "thread_safe"},
		{"deferred", "thread_safe"}, {"tool", "game_only"}} {
		if a := found[pair[1]]; a != nil && found[pair[0]] != nil {
			return nil, u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotations @%s and @%s can't be used together.", pair[0], pair[1]), "")
		}
	}
	return found, nil
}

// debugOn reports whether a, a @trace or @profile annotation in the class named class, is in a group that the
// options turn on: the class's name, a group its arguments name, or all. False if a is nil.
func (u *unit) debugOn(a *Annotation, class string) (bool, error) {
	if a == nil {
		return false, nil
	}
	groups := []string{class, "all"}
	for _, arg := range a.Args {
		name, err := strconv.Unquote(arg.Value)
		if err != nil || !identRegexp.MatchString(name) {
			return false, u.errorAt(arg.Pos, len(arg.Value), fmt.Sprintf("Annotation @%s takes names of groups in double quotes, e.g. @%s(\"combat\").", a.Name, a.Name), "")
		}
		groups = append(groups, name)
	}
	on := u.opts.Trace
	if a.Name == "profile" {
		on = u.opts.Profile
	}
	return slices.ContainsFunc(groups, func(g string) bool { return slices.Contains(on, g) }), nil
}

// buildFunc checks f, a function of the class or extern named owner.
func (u *unit) buildFunc(f *Func, owner string, ext bool) (*funcModel, error) {
	allowed := []string{"const", "deferred", "override", "profile", "rpc", "static", "thread_safe", "trace", "virtual"}
	if ext {
		allowed = []string{"const", "deferred", "rpc", "thread_safe"}
	}
	a, err := u.annotations(f.Annotations, "a func", allowed...)
	if err != nil {
		return nil, err
	}
	m := &funcModel{f: f, virtual: a["virtual"] != nil, override: a["override"] != nil, isConst: a["const"] != nil,
		static: a["static"] != nil}
	if m.trace, err = u.debugOn(a["trace"], owner); err != nil {
		return nil, err
	}
	if m.profile, err = u.debugOn(a["profile"], owner); err != nil {
		return nil, err
	}
	for _, name := range []string{"deferred", "thread_safe"} {
		if a[name] != nil {
			m.deferral = name
		}
	}
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
		if err := u.enumShorthand(t, (*Init)(p.Default)); err != nil {
			return nil, err
		}
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
	case m.deferral != "" && !m.ret.void:
		return nil, u.errorAt(f.Pos, 4, fmt.Sprintf("The @%s function %s must return void.", m.deferral, f.Name), "Its calls run later, so they can't return a value.")
	}
	if err := u.requireBase(a["thread_safe"], owner, "call_thread_safe is a method of Node.", "Node"); err != nil {
		return nil, err
	}
	if err := u.requireBase(a["rpc"], owner, "rpc and rpc_config are methods of Node.", "Node"); err != nil {
		return nil, err
	}
	return m, nil
}

// requireBase returns an error at annotation a, if set, unless the class or extern named owner is one of bases or
// extends one of them.
func (u *unit) requireBase(a *Annotation, owner, hint string, bases ...string) error {
	for name, seen := owner, 0; a != nil && seen < 100; seen++ {
		s := u.symbols[name]
		switch {
		case slices.Contains(bases, name):
			return nil
		case s == nil: // Also a dependency whose base isn't known.
			return u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotation @%s can only be used in classes that extend %s, which %s doesn't.",
				a.Name, strings.Join(bases, " or "), owner), hint)
		case s.class != nil:
			name = baseName(s.class.Extends)
		case s.extern != nil:
			name = baseName(s.extern.Extends)
		default:
			name = s.base
		}
	}
	return nil
}

// bodyName is the name of the method that holds the body of a class's @deferred or @thread_safe func f.
func bodyName(f *funcModel) string {
	return "_gdpp_body_" + f.f.Name
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

// buildSignal checks s, a signal of the class or extern named owner.
func (u *unit) buildSignal(s *Signal, owner string, ext bool) (*signalModel, error) {
	allowed := []string{"trace"}
	if ext {
		allowed = nil
	}
	a, err := u.annotations(s.Annotations, "a signal", allowed...)
	if err != nil {
		return nil, err
	}
	m := &signalModel{s: s}
	if m.trace, err = u.debugOn(a["trace"], owner); err != nil {
		return nil, err
	}
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

// buildVar checks v, a variable of the class or extern named owner.
func (u *unit) buildVar(v *Var, owner string, ext bool) (*varModel, error) {
	allowed := append([]string{"onready", "export", "export_dir", "export_enum", "export_file", "export_flags",
		"export_multiline", "export_placeholder", "export_range", "export_storage", "profile", "trace"}, sectionAnnotations...)
	if ext {
		allowed = nil
	}
	a, err := u.annotations(v.Annotations, "a var", allowed...)
	if err != nil {
		return nil, err
	}
	m := &varModel{v: v, onready: a["onready"] != nil, usage: "PROPERTY_USAGE_NONE", hint: "PROPERTY_HINT_NONE"}
	if m.trace, err = u.debugOn(a["trace"], owner); err != nil {
		return nil, err
	}
	if m.profile, err = u.debugOn(a["profile"], owner); err != nil {
		return nil, err
	}
	if m.t, err = u.resolve(v.Type, false); err != nil {
		return nil, err
	}
	if ext && (v.Init != nil || v.Property != nil) {
		return nil, u.errorAt(v.Pos, 3, "Extern variables can't have an initial value or a property body.", "Externs only declare what another package defines.")
	}
	if err := u.enumShorthand(m.t, v.Init); err != nil {
		return nil, err
	}
	if err := u.exportHint(m); err != nil {
		return nil, err
	}
	if err := u.sections(m); err != nil {
		return nil, err
	}
	if err := u.requireBase(a["onready"], owner, "Only nodes get _ready.", "Node"); err != nil {
		return nil, err
	}
	for _, e := range v.Annotations {
		if strings.HasPrefix(e.Name, "export") {
			if err := u.requireBase(e, owner, "Only nodes and resources are edited in the inspector.", "Node", "Resource"); err != nil {
				return nil, err
			}
		}
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
		if e := m.t.enum; e != nil {
			var names []string
			for _, v := range e.values {
				if !e.bitfield || v.Value > 0 && v.Value&(v.Value-1) == 0 { // Bitfields list their single-bit flags.
					names = append(names, fmt.Sprintf("%s:%d", capitalize(v.Name), v.Value))
				}
			}
			m.hint, m.hintString = "PROPERTY_HINT_ENUM", strings.Join(names, ",")
			if e.bitfield {
				m.hint = "PROPERTY_HINT_FLAGS"
			}
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
				if f, err = u.buildFunc(member.Func, e.Name, true); err == nil {
					m.funcs = append(m.funcs, f)
					err = u.unique(names, f.f.Pos, "func", f.f.Name)
				}
			case member.Var != nil:
				var v *varModel
				if v, err = u.buildVar(member.Var, e.Name, true); err == nil {
					m.vars = append(m.vars, v)
					err = u.unique(names, v.v.Pos, "var", v.v.Name, v.getter, v.setter)
				}
			case member.Signal != nil:
				var sig *signalModel
				if sig, err = u.buildSignal(member.Signal, e.Name, true); err == nil {
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
	a, err := u.annotations(c.Annotations, "a class", "game_only", "icon", "profile", "tool", "trace")
	if err != nil {
		return nil, err
	}
	m.gameOnly = a["game_only"] != nil
	if m.trace, err = u.debugOn(a["trace"], c.Name); err != nil {
		return nil, err
	}
	if m.profile, err = u.debugOn(a["profile"], c.Name); err != nil {
		return nil, err
	}
	if _, err := u.classIcon(c); err != nil {
		return nil, err
	}
	names := map[string]bool{}
	var declared []*symbol // Enums declared in the class.
	for _, member := range c.Members {
		var err error
		switch {
		case member.Code != nil:
			keyword, _ := member.keyword()
			var a map[string]*Annotation
			a, err = u.annotations(member.Code.Annotations, "a "+keyword+" block", "global")
			if a["global"] != nil {
				m.globals = append(m.globals, member.Code)
			} else {
				m.codes = append(m.codes, member.Code)
			}
		case member.Ctor != nil && m.ctor != nil, member.Dtor != nil && m.dtor != nil:
			keyword, pos := member.keyword()
			return nil, u.errorAt(pos, len(keyword), fmt.Sprintf("Class %s has two %ss.", c.Name, keyword), "")
		case member.Ctor != nil:
			m.ctor = member.Ctor
		case member.Dtor != nil:
			m.dtor = member.Dtor
		case member.Func != nil:
			var f *funcModel
			if f, err = u.buildFunc(member.Func, c.Name, false); err == nil {
				m.funcs = append(m.funcs, f)
				err = u.unique(names, f.f.Pos, "func", f.f.Name)
			}
			if err == nil && f.deferral != "" {
				// The deferred call runs the body, a separate method.
				body := *f.f
				body.Name, body.Annotations, body.Doc = bodyName(f), nil, nil
				body.Params = nil
				for _, p := range f.f.Params {
					p := *p
					p.Default = nil
					body.Params = append(body.Params, &p)
				}
				// The body does the work, so it's what @trace and @profile follow.
				m.funcs = append(m.funcs, &funcModel{f: &body, params: f.params, ret: f.ret, isConst: f.isConst, hidden: true,
					trace: f.trace || m.trace, profile: f.profile || m.profile})
				f.trace, f.profile = false, false
				err = u.unique(names, f.f.Pos, "func", body.Name)
			}
		case member.Var != nil:
			var v *varModel
			if v, err = u.buildVar(member.Var, c.Name, false); err == nil {
				v.trace = v.trace || m.trace
				v.profile = v.profile || m.profile && v.v.Property != nil
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
			if sig, err = u.buildSignal(member.Signal, c.Name, false); err == nil {
				sig.trace = sig.trace || m.trace
				m.signals = append(m.signals, sig)
				err = u.unique(names, sig.s.Pos, "signal", sig.s.Name)
			}
		case member.Enum != nil && member.Enum.Value != nil:
			if _, err = u.annotations(member.Enum.Annotations, "a constant"); err == nil {
				m.consts = append(m.consts, member.Enum)
				err = u.unique(names, member.Enum.Pos, "enum", member.Enum.Name)
			}
		case member.Enum != nil:
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
	// The class's @trace leaves out the functions called every frame, which would flood the output.
	for _, f := range m.funcs {
		if f.deferral == "" {
			f.trace = f.trace || m.trace && !(f.override && processing[f.f.Name] != "")
			f.profile = f.profile || m.profile
		}
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
