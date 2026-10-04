package syntax_0

import (
	"cmp"
	"fmt"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf16"
	"unicode/utf8"

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
	tracing bool // Whether the unit's header defines GDPP_TRACING, so that failed assertions print trace lines.
}

type classModel struct {
	name       string
	cls        *Class
	base       string
	refCounted bool
	gameOnly   bool       // Whether @game_only guards all its code against running in the editor.
	trace      bool       // Whether its @trace is on: it traces its lifetime, signals, and all its funcs and vars.
	profile    bool       // Whether its @profile is on: it profiles all its funcs, and the get and set blocks of its vars.
	codes      []*Code    // decl and impl blocks inside the class.
	globals    []*Code    // @global decl and impl blocks, outside the class and namespace godot.
	pool       *poolModel // Its @pool, or nil.
	scene      string     // The res:// path of its @scene, or "".
	ctor, dtor *Block
	notifs     []*notifModel
	funcs      []*funcModel
	vars       []*varModel
	signals    []*signalModel
	consts     []*Enum
	enums      []*symbol // Enums the class exposes its own copy of.
	imports    []*Type
	noimports  []*Type

	// A @pool class's @recycle ctor and dtor blocks, which run when its pool reuses and keeps an object, and also right
	// after the constructor and before the destructor, unless they have @recycle("only").
	recycleCtor, recycleDtor         *Block
	recycleCtorOnly, recycleDtorOnly bool
	// Whether a @pool class has @onready values or a _ready without @recycle, which only run the first time an
	// object gets ready.
	readyOnce bool
}

// notifModel is a handler in the class's _notification: the call of an on block.
type notifModel struct {
	cond   string // The C++ condition on WHAT.
	call   string // The on block's call, e.g. "_gdpp_body__process(get_process_delta_time())".
	setter string // For process and the like: the method that turns processing on.
}

// poolModel is the pool of a @pool class.
type poolModel struct {
	size string // How many objects it expects, as C++, or "0" for none.
	mode string // What it does when all of them are in use: "fixed" or "grow", like gdpp::PoolMode.
}

type externModel struct {
	name       string
	ext        *Extern
	base       string
	refCounted bool
	trace      bool // Whether its @trace is on: it traces all its funcs and signals.
	profile    bool // Whether its @profile is on: it profiles all its funcs, except deferred ones, and vars.
	scene      bool // Whether it has @scene: its class has a scene, which create instantiates.
	pool       bool // Whether it has @pool: its class has a pool, which create, destroy and queue_destroy use.
	funcs      []*funcModel
	vars       []*varModel
	signals    []*signalModel
}

type funcModel struct {
	f                                  *Func
	params                             []*gtype
	ret                                *gtype
	virtual, override, isConst, static bool
	final, super, private              bool        // With @override("final"), "super" on @override or @virtual, and @virtual("private").
	calls                              *funcModel  // Called as the whole body: for "super", the bound function with the body, for the caller of a @virtual function, that function.
	deferral                           string      // "deferred", "thread_safe" or "onthread" with that annotation, else empty.
	detached                           bool        // With @onthread("detached"): a call starts a task that nobody waits for, and returns nothing.
	hidden                             string      // For the generated body of a class's func with a deferral: that deferral. "notif" for an on block.
	trace, profile                     bool        // Whether its @trace or @profile, or its class's, is on.
	notrace, noprofile                 bool        // Whether it has @notrace or @noprofile, which leave it out of its class's.
	gameOnly                           bool        // Whether its @game_only, or its class's, guards it against running in the editor.
	rpc                                *rpcModel   // Nil without @rpc.
	recycle                            *Annotation // On _ready, its @recycle, or nil.
	once                               bool        // Whether it's a _ready of a @pool class without @recycle, which only runs the first time an object gets ready.
	// The class whose GDVIRTUAL lets scripts override the function: its own for @virtual, a base's for an
	// @override of a @virtual function. Empty for others.
	virtualOf string
}

// rpcModel is the configuration from @rpc, as C++ values. Empty in externs, whose defining class configures it.
type rpcModel struct {
	mode, transfer string // E.g. "MultiplayerAPI::RPC_MODE_ANY_PEER".
	callLocal      bool
	channel        string
}

// trampolined reports whether the function's signature mentions an enum, so it's bound through a trampoline. The
// body of an @onthread func isn't bound at all.
func (f *funcModel) trampolined() bool {
	return f.hidden != "onthread" && (f.ret.enum != nil || slices.ContainsFunc(f.params, func(t *gtype) bool { return t.enum != nil }))
}

type varModel struct {
	v                *Var
	t                *gtype
	onready          bool
	notrace          bool        // Whether it has @notrace, which leaves it out of its class's @trace.
	noprofile        bool        // Whether it has @noprofile, which leaves it out of its class's @profile.
	recycle          *Annotation // Its @recycle, which resets it when its class's pool reuses an object, or nil.
	trace            bool        // Whether its @trace, or its class's, is on: the class's funcs print its changes.
	profile          bool        // Whether its @profile, or its class's, is on: it profiles its getter and setter.
	gameOnly         bool        // Whether its @game_only, or its class's, guards its getter and setter against running in the editor.
	usage            string      // A PROPERTY_USAGE_* expression.
	hint, hintString string      // A PROPERTY_HINT_* name, and the hint string (not quoted).
	getter, setter   string      // Empty if there is none.
	get              *Block
	set              *Setter
	deferral         string // "deferred" or "thread_safe" with that annotation on its set block, else empty.
	decls            []*Block
	sections         []section // In source order.
}

// section is an inspector section a var starts, from a section annotation.
type section struct {
	ann          *Annotation
	name, prefix string
}

type signalModel struct {
	s       *Signal
	params  []*gtype
	trace   bool // Whether its @trace, or its class's, is on: emitting it prints it.
	notrace bool // Whether it has @notrace, which leaves it out of its class's @trace.
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
				Tool: hasAnnotation(c, "tool"), GameOnly: hasAnnotation(c, "game_only"), Async: usesAsync(c),
				Virtuals: classVirtuals(c), Notifications: ownNotifications(c.Members)})
		case s.extern != nil:
			decls = append(decls, meta.Declaration{Name: s.name, Kind: meta.ExternDecl, Base: baseName(s.extern.Extends)})
		default:
			decls = append(decls, meta.Declaration{Name: s.name, Kind: meta.EnumDecl, Base: s.base, Values: s.values, Bitfield: s.bitfield})
		}
	}
	return decls, nil
}

// usesAsync reports whether class c uses Async: has an @onthread function, or an Async type in a signature.
func usesAsync(c *Class) bool {
	isAsync := func(t *Type) bool { return t != nil && t.Name == "Async" }
	hasAsync := func(params []*Param) bool {
		return slices.ContainsFunc(params, func(p *Param) bool { return isAsync(p.Type) })
	}
	return slices.ContainsFunc(c.Members, func(m *Member) bool {
		switch {
		case m.Func != nil:
			return isAsync(m.Func.Return) || hasAsync(m.Func.Params) ||
				slices.ContainsFunc(m.Func.Annotations, func(a *Annotation) bool { return a.Name == "onthread" })
		case m.Signal != nil:
			return hasAsync(m.Signal.Params)
		case m.Var != nil:
			return isAsync(m.Var.Type)
		}
		return false
	})
}

// hasAnnotation reports whether class c has the annotation named name.
func hasAnnotation(c *Class, name string) bool {
	return slices.ContainsFunc(c.Annotations, func(a *Annotation) bool { return a.Name == name })
}

// classVirtuals returns the names of the @virtual functions of class c.
func classVirtuals(c *Class) []string {
	var names []string
	for _, m := range c.Members {
		if m.Func != nil && slices.ContainsFunc(m.Func.Annotations, func(a *Annotation) bool { return a.Name == "virtual" }) {
			names = append(names, m.Func.Name)
		}
	}
	return names
}

// ownNotifications returns the names of the notifications that members declare, without NOTIFICATION_: those of
// their constants named NOTIFICATION_..., e.g. HIT for enum NOTIFICATION_HIT = 2000.
func ownNotifications(members []*Member) []string {
	var names []string
	for _, m := range members {
		if e := m.Enum; e != nil && e.Value != nil {
			if name, ok := strings.CutPrefix(e.Name, "NOTIFICATION_"); ok {
				names = append(names, name)
			}
		}
	}
	return names
}

// notificationOf returns the class that has the notification name, e.g. READY: the class named class, or one of
// its bases. If there's none, it returns "" and the notifications that they have.
func (u *unit) notificationOf(class, name string) (string, []string) {
	var all []string
	for class != "" {
		s, own := u.symbols[class], []string(nil)
		switch {
		case s == nil:
			return "", all
		case s.class != nil:
			own = ownNotifications(s.class.Members)
		case s.extern != nil:
			own = ownNotifications(s.extern.Members)
		default:
			own = s.notifications
		}
		if slices.Contains(own, name) {
			return class, nil
		}
		all = append(all, own...)
		switch {
		case s.class != nil:
			class = baseName(s.class.Extends)
		case s.extern != nil:
			class = baseName(s.extern.Extends)
		default:
			class = s.base
		}
	}
	return "", all
}

// setVirtualOf sets f.virtualOf for f, a function of the class named class with base base.
func (u *unit) setVirtualOf(f *funcModel, class, base string) error {
	owner, gdpp := u.virtualOwner(base, f.f.Name)
	notifOwner, _ := u.notificationOf(class, strings.ToUpper(strings.TrimPrefix(f.f.Name, "_")))
	isNotif := strings.HasPrefix(f.f.Name, "_") && notifOwner != "" // E.g. _ready, for which on ready is better.
	switch {
	case f.virtual && owner != "":
		a := f.f.Annotations[slices.IndexFunc(f.f.Annotations, func(a *Annotation) bool { return a.Name == "virtual" })]
		return u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Function %s is already virtual in %s.", f.f.Name, owner), "Use @override to override it.")
	case !f.virtual && !f.override && owner != "":
		hint := "Add @override, or rename the function."
		if isNotif {
			hint = fmt.Sprintf("To add code that runs at its notification, write \"%s\" instead. Only to replace the base's %s, which a script replaces in turn, add @override.", onExample(f.f.Name[1:]), f.f.Name)
		}
		return u.errorAt(f.f.Pos, 4, fmt.Sprintf("Function %s overrides a virtual function of %s, so it needs @override.", f.f.Name, owner), hint)
	case f.final && !gdpp:
		a := f.f.Annotations[slices.IndexFunc(f.f.Annotations, func(a *Annotation) bool { return a.Name == "override" })]
		arg := a.Args[slices.IndexFunc(a.Args, func(arg *Arg) bool { return arg.Value == `"final"` })]
		return u.errorAt(arg.Pos, len(arg.Value), "Annotation @override(\"final\") only works on overrides of a GD++ @virtual function.",
			"The engine lets scripts override its own virtual functions, so GD++ can't stop that.")
	case f.virtual:
		f.virtualOf = class
	case f.override && !f.final && gdpp: // Without a GDVIRTUAL_CALL, scripts' overrides never run.
		f.virtualOf = owner
	}
	return nil
}

// virtualOwner returns the class that declares the virtual function name, the class named class or one of its
// bases, and whether it's a GD++ class, whose @virtual declares it, rather than an engine class. Empty if there's
// none. Bases don't cycle: kindOf rejects that first.
func (u *unit) virtualOwner(class, name string) (string, bool) {
	for class != "" {
		s := u.symbols[class]
		switch {
		case s == nil:
			return "", false
		case s.class != nil:
			if slices.Contains(classVirtuals(s.class), name) {
				return class, true
			}
			class = baseName(s.class.Extends)
		default:
			if slices.Contains(s.virtuals, name) {
				return class, s.gdpp
			}
			class = s.base
		}
	}
	return "", false
}

// classIcon returns the path from the @icon annotation of class c, or "" if it has none.
func (u *unit) classIcon(c *Class) (string, error) {
	for _, a := range c.Annotations {
		if a.Name == "icon" {
			return u.classPath(a, "icon", "res://icons/player.svg")
		}
	}
	return "", nil
}

// classPath returns the path that a, a class's @icon or @scene annotation, takes: a res:// or pkg:// path, as a
// string. what names the file, e.g. "icon", and example is a path for the hint.
func (u *unit) classPath(a *Annotation, what, example string) (string, error) {
	if len(a.Args) != 1 || !strings.HasPrefix(a.Args[0].Value, "\"") {
		return "", u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotation @%s needs one argument: the %s's path, as a string.", a.Name, what),
			fmt.Sprintf("E.g. \"@%s(\\\"%s\\\")\".", a.Name, example))
	}
	arg := a.Args[0]
	path, err := strconv.Unquote(arg.Value)
	if err != nil || !strings.HasPrefix(path, "res://") && !strings.HasPrefix(path, "pkg://") {
		return "", u.errorAt(arg.Pos, len(arg.Value), fmt.Sprintf("The %s's path must start with res:// or pkg://.", what),
			"res:// paths are relative to the project, pkg:// paths to the package.")
	}
	return path, nil
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
		s := &symbol{name: d.Name, kind: d.Kind, include: d.Include, values: d.Values, base: d.Base, gdpp: d.Gdpp, bitfield: d.Bitfield,
			virtuals: d.Virtuals, notifications: d.Notifications}
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

// nodeShorthand turns the initial value of m into C++ if it's a node path, e.g. $Hud/Score or %Health, which becomes
// get_node<Label>("Hud/Score"). The node only has children once it's ready, so m needs @onready, and a node type.
func (u *unit) nodeShorthand(m *varModel) error {
	v, init := m.v, m.v.Init
	if init == nil || init.Block != nil || init.Expr == "" || init.Expr[0] != '$' && init.Expr[0] != '%' {
		return nil
	}
	path, off, msg, hint := nodePath(init.Expr)
	if msg != "" {
		pos := init.Pos
		pos.Offset, pos.Column = pos.Offset+off, pos.Column+utf8.RuneCountInString(init.Expr[:off])
		return u.errorAt(pos, 1, msg, hint)
	}
	if !m.onready {
		return u.errorAt(init.Pos, len(init.Expr), "A node path needs @onready, since the node has no children yet in the constructor.", "Add @onready.")
	}
	const need, needHint = "A node path needs a variable of a node type, e.g. Node3D, but %s %s.", "$ and % get nodes."
	if v.Type == nil {
		return u.errorAt(init.Pos, len(init.Expr), fmt.Sprintf(need, v.Name, "has no type"), needHint)
	}
	if m.t.cpp != v.Type.Name+" *" || !u.extends(v.Type.Name, "Node") {
		return u.errorAt(v.Type.Pos, len(v.Type.Name), fmt.Sprintf(need, v.Name, "has type "+m.t.doc), needHint)
	}
	init.Expr = fmt.Sprintf("get_node<%s>(%s)", v.Type.Name, cppString(path))
	return nil
}

// nodePath parses expr, a node path with GDScript's rules: "$" or "%", then names or strings joined by "/", where
// "%" may start a name, and "$" may be followed by "/", e.g. $Hud/Score, %Health, $/root/Main or $"../Sibling".
// It returns the path, e.g. "Hud/Score" or "%Health", or the offset in expr of the first error, its message and hint.
func nodePath(expr string) (path string, off int, msg, hint string) {
	var sb strings.Builder
	last, i := expr[:1], 1 // The last "$", "%" or "/", or "" after a name.
	if last == "%" {
		sb.WriteString(last)
	} else if j := skipBlanks(expr, i); j < len(expr) && expr[j] == '/' {
		sb.WriteByte('/')
		last, i = "/", j+1
	}
	for {
		i = skipBlanks(expr, i)
		name, n, off, msg := nodeName(expr[i:])
		if msg != "" {
			return "", i + off, msg, ""
		}
		if n > 0 {
			sb.WriteString(name)
			last, i = "", skipBlanks(expr, i+n)
		}
		switch {
		case (i == len(expr) || expr[i] != '/' && expr[i] != '%') && last != "":
			return "", i, fmt.Sprintf("Expected a node name or a string after %q.", last), ""
		case i < len(expr) && expr[i] != '/' && expr[i] != '%':
			return "", i, "A node path must be the whole initial value.", `In C++ code, use get_node<T>("Path").`
		case i == len(expr):
			return sb.String(), 0, "", ""
		case expr[i] == '%' && last != "$" && last != "/":
			return "", i, `A "%" is only valid at the start of a node name, after "$" or "/".`, ""
		case expr[i] == '/' && last != "$" && last != "":
			return "", i, `A "/" is only valid at the start of the path, or after a node name.`, ""
		}
		last = expr[i : i+1]
		sb.WriteString(last)
		i++
	}
}

// skipBlanks returns the index of the first byte at or after i in s that isn't a space or a tab.
func skipBlanks(s string, i int) int {
	for i < len(s) && strings.IndexByte(" \t\r", s[i]) >= 0 {
		i++
	}
	return i
}

// nodeName parses the node name at the start of s, like GDScript: an identifier or a keyword, but not the literals
// true, false and null, or a string. It returns the name and its length, 0 if there's none, or the offset and message
// of an error.
func nodeName(s string) (name string, n, off int, msg string) {
	if strings.HasPrefix(s, `"`) || strings.HasPrefix(s, "'") || strings.HasPrefix(s, `r"`) || strings.HasPrefix(s, "r'") {
		return gdscriptString(s)
	}
	for n < len(s) {
		r, size := utf8.DecodeRuneInString(s[n:])
		if r != '_' && !unicode.In(r, unicode.L, unicode.Nl) && (n == 0 || !unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc)) {
			break
		}
		n += size
	}
	if name = s[:n]; name == "true" || name == "false" || name == "null" {
		return "", 0, 0, ""
	}
	return name, n, 0, ""
}

// gdscriptString parses the GDScript string at the start of s: "...", '...', triple-quoted, or raw, e.g. r"...".
// It returns its value and its length, or the offset and message of an error.
func gdscriptString(s string) (value string, n, off int, msg string) {
	raw := s[0] == 'r'
	if raw {
		n = 1
	}
	quote := s[n : n+1]
	if strings.HasPrefix(s[n:], strings.Repeat(quote, 3)) {
		quote = strings.Repeat(quote, 3)
	}
	var sb strings.Builder
	for n += len(quote); ; {
		switch {
		case n == len(s):
			return "", 0, 0, "This string is never closed."
		case strings.HasPrefix(s[n:], quote):
			return sb.String(), n + len(quote), 0, ""
		case s[n] != '\\':
			sb.WriteByte(s[n])
			n++
		case raw: // Raw strings keep their escapes, but \" (with their quote) and \\ don't end them.
			size := 1
			if n+1 < len(s) && (s[n+1] == quote[0] || s[n+1] == '\\') {
				size = 2
			}
			sb.WriteString(s[n : n+size])
			n += size
		default:
			r, size := gdscriptEscape(s[n:])
			if size == 0 {
				return "", 0, n, "Invalid escape in string."
			}
			sb.WriteRune(r)
			n += size
		}
	}
}

// gdscriptEscape decodes the GDScript escape at the start of s, e.g. \n or é, where a \u pair may encode a
// UTF-16 surrogate pair. It returns the character and the escape's length, 0 if it's invalid.
func gdscriptEscape(s string) (rune, int) {
	if len(s) < 2 {
		return 0, 0
	}
	if i := strings.IndexByte(`tnrabfv"'\`, s[1]); i >= 0 {
		return rune("\t\n\r\a\b\f\v\"'\\"[i]), 2
	}
	hex := func(s string) (rune, int) { // \uXXXX or \UXXXXXX.
		digits := 0
		if len(s) > 1 && s[0] == '\\' {
			digits = map[byte]int{'u': 4, 'U': 6}[s[1]]
		}
		if digits == 0 || len(s) < 2+digits {
			return 0, 0
		}
		v, err := strconv.ParseUint(s[2:2+digits], 16, 32)
		if err != nil {
			return 0, 0
		}
		return rune(v), 2 + digits
	}
	r, n := hex(s)
	if n == 0 || r > unicode.MaxRune || r >= 0xdc00 && r < 0xe000 {
		return 0, 0
	}
	if utf16.IsSurrogate(r) { // A lead surrogate, which needs a trail one.
		trail, size := hex(s[n:])
		if r = utf16.DecodeRune(r, trail); size == 0 || r == unicode.ReplacementChar {
			return 0, 0
		}
		n += size
	}
	return r, n
}

// cppString returns s as a C++ string literal for a NodePath: U"..." if it isn't ASCII, since godot-cpp reads plain
// literals as Latin-1.
func cppString(s string) string {
	var sb strings.Builder
	if strings.IndexFunc(s, func(r rune) bool { return r >= utf8.RuneSelf }) >= 0 {
		sb.WriteByte('U')
	}
	sb.WriteByte('"')
	for _, b := range []byte(s) {
		switch {
		case b == '"' || b == '\\':
			sb.WriteString(`\` + string(b))
		case b < ' ' || b == 0x7f:
			fmt.Fprintf(&sb, `\%03o`, b) // Octal, since \x takes any number of digits.
		default:
			sb.WriteByte(b)
		}
	}
	sb.WriteByte('"')
	return sb.String()
}

var identRegexp = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

var knownAnnotations = []string{"bitfield", "const", "deferred", "export", "export_category", "export_dir", "export_enum", "export_file", "export_flags",
	"export_group", "export_multiline", "export_placeholder", "export_range", "export_storage", "export_subgroup", "game_only", "global", "icon", "noprofile", "notrace", "onready",
	"onthread", "override", "pool", "profile", "recycle", "rpc", "scene", "static", "thread_safe", "tool", "trace", "virtual"}

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
			"export_placeholder", "export_range", "export_subgroup", "icon", "onthread", "override", "pool", "profile", "recycle", "rpc", "scene", "trace",
			"virtual"}, a.Name) || a.Name == "recycle" && len(a.Args) > 0 && kind != "a ctor block" && kind != "a dtor block":
			return nil, u.errorAt(a.Args[0].Pos, len(a.Args[0].Value), fmt.Sprintf("Annotation @%s takes no arguments.", a.Name), "")
		}
		found[a.Name] = a
	}
	for _, pair := range [][2]string{{"static", "virtual"}, {"static", "override"}, {"static", "const"}, {"virtual", "override"},
		{"static", "rpc"}, {"virtual", "rpc"}, {"override", "rpc"}, {"static", "deferred"}, {"virtual", "deferred"},
		{"override", "deferred"}, {"static", "thread_safe"}, {"virtual", "thread_safe"}, {"override", "thread_safe"},
		{"deferred", "thread_safe"}, {"virtual", "onthread"}, {"override", "onthread"}, {"rpc", "onthread"},
		{"deferred", "onthread"}, {"thread_safe", "onthread"}, {"tool", "game_only"}} {
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
	allowed := []string{"const", "deferred", "game_only", "noprofile", "notrace", "onthread", "override", "profile", "recycle", "rpc", "static",
		"thread_safe", "trace", "virtual"}
	if ext {
		allowed = []string{"const", "deferred", "noprofile", "notrace", "profile", "rpc", "thread_safe", "trace"}
	}
	kind := "a func"
	if ext {
		kind = "an extern func"
	}
	a, err := u.annotations(f.Annotations, kind, allowed...)
	if err != nil {
		return nil, err
	}
	m := &funcModel{f: f, virtual: a["virtual"] != nil, override: a["override"] != nil,
		isConst: a["const"] != nil, static: a["static"] != nil, gameOnly: a["game_only"] != nil, recycle: a["recycle"]}
	for _, arg := range argsOf(a["override"]) {
		name, _ := strconv.Unquote(arg.Value)
		switch {
		case name == "engine":
			return nil, u.errorAt(arg.Pos, len(arg.Value), "Annotation @override no longer takes \"engine\".",
				fmt.Sprintf("Drop it: a plain @override already makes %s a plain override.", f.Name))
		case name != "final" && name != "super":
			return nil, u.errorAt(arg.Pos, len(arg.Value), "Annotation @override takes \"final\" or \"super\".", "E.g. @override(\"final\").")
		case name == "final" && m.final, name == "super" && m.super:
			return nil, u.errorAt(arg.Pos, len(arg.Value), fmt.Sprintf("Annotation @override takes %s only once.", arg.Value), "")
		}
		m.final = m.final || name == "final"
		m.super = m.super || name == "super"
	}
	for _, arg := range argsOf(a["onthread"]) {
		name, _ := strconv.Unquote(arg.Value)
		switch {
		case name != "detached":
			return nil, u.errorAt(arg.Pos, len(arg.Value), "Annotation @onthread takes \"detached\".", "E.g. @onthread(\"detached\").")
		case m.detached:
			return nil, u.errorAt(arg.Pos, len(arg.Value), fmt.Sprintf("Annotation @onthread takes %s only once.", arg.Value), "")
		}
		m.detached = true
	}
	for _, arg := range argsOf(a["virtual"]) {
		name, _ := strconv.Unquote(arg.Value)
		switch {
		case name != "private" && name != "super":
			return nil, u.errorAt(arg.Pos, len(arg.Value), "Annotation @virtual takes \"private\" or \"super\".", "E.g. @virtual(\"private\").")
		case name == "private" && m.private, name == "super" && m.super:
			return nil, u.errorAt(arg.Pos, len(arg.Value), fmt.Sprintf("Annotation @virtual takes %s only once.", arg.Value), "")
		}
		m.private = m.private || name == "private"
		m.super = m.super || name == "super"
	}
	if m.virtual && (!strings.HasPrefix(f.Name, "_") || f.Name == "_") {
		hint := fmt.Sprintf("Like Godot's virtual functions. Scripts call it without the \"_\", e.g. _%s as %s.", f.Name, f.Name)
		if m.private {
			hint = "Like Godot's virtual functions."
		}
		return nil, u.errorAt(f.Pos, 4, fmt.Sprintf("The @virtual function %s must have a name that starts with \"_\", e.g. _%s.", f.Name, f.Name), hint)
	}
	if m.trace, err = u.debugOn(a["trace"], owner); err != nil {
		return nil, err
	}
	if m.profile, err = u.debugOn(a["profile"], owner); err != nil {
		return nil, err
	}
	m.notrace, m.noprofile = a["notrace"] != nil, a["noprofile"] != nil
	for _, name := range []string{"deferred", "thread_safe", "onthread"} {
		if a[name] != nil {
			m.deferral = name
		}
	}
	if p := a["profile"]; ext && p != nil && m.deferral != "" {
		return nil, u.errorAt(p.Pos, len(p.Name)+1, fmt.Sprintf("Annotation @profile can't be used on @%s functions of externs.", m.deferral),
			"A call through the extern only queues the function, so there's nothing to time. Profile the class that defines it instead.")
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
	case m.detached && !m.ret.void:
		return nil, u.errorAt(f.Pos, 4, fmt.Sprintf("The @onthread(\"detached\") function %s must return void.", f.Name),
			"Nothing waits for its task, so its calls can't return a value.")
	case m.deferral == "onthread" && m.ret.async != nil:
		return nil, u.errorAt(f.Return.Pos, len(f.Return.Name), fmt.Sprintf("The @onthread function %s already returns an Async: write -> %s.", f.Name, typeString(asyncArg(f.Return))),
			"Its callers get an Async of the type its body returns.")
	case (m.deferral == "deferred" || m.deferral == "thread_safe") && !m.ret.void:
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

// onParams are the on blocks that take a parameter, by name: the parameter's type, and its C++ value. The nameless
// block runs at every notification.
var onParams = map[string]struct{ typ, value string }{
	"":                         {"int", "WHAT"},
	"process":                  {"float", "get_process_delta_time()"},
	"physics_process":          {"float", "get_physics_process_delta_time()"},
	"internal_process":         {"float", "get_process_delta_time()"},
	"internal_physics_process": {"float", "get_physics_process_delta_time()"},
}

// onExample returns how the on block called name is written, e.g. "on process(delta: float) { ... }".
func onExample(name string) string {
	switch p, ok := onParams[name]; {
	case name == "":
		return "on(what: int) { ... }"
	case ok:
		return fmt.Sprintf("on %s(delta: %s) { ... }", name, p.typ)
	}
	return "on " + name + " { ... }"
}

// buildOn checks o, an on block of the class named owner, and returns it as the function that its notification
// calls, named after it, e.g. _ready for on ready { ... }, or _notification for the nameless block.
func (u *unit) buildOn(o *On, owner string) (*funcModel, error) {
	what := "an on block"
	if o.Name != "" {
		what = "an on " + o.Name + " block"
	}
	allowed := []string{"game_only", "noprofile", "notrace", "profile", "trace"}
	if o.Name == "ready" {
		allowed = append(allowed, "recycle")
	}
	if _, err := u.annotations(o.Annotations, what, allowed...); err != nil {
		return nil, err
	}
	param, takesParam := onParams[o.Name]
	switch upper := strings.ToUpper(o.Name); {
	case strings.HasPrefix(upper, "NOTIFICATION_"):
		short := strings.ToLower(upper[len("NOTIFICATION_"):])
		return nil, u.errorAt(o.Pos, len(o.Name)+3, fmt.Sprintf("Write notification %s without NOTIFICATION_.", o.Name),
			fmt.Sprintf("GD++ adds the prefix: \"%s\".", onExample(short)))
	case o.Name != strings.ToLower(o.Name):
		return nil, u.errorAt(o.Pos, len(o.Name)+3, fmt.Sprintf("Write notification %s in lower case.", o.Name),
			fmt.Sprintf("E.g. \"%s\".", onExample(strings.ToLower(o.Name))))
	case o.Name != "":
		if class, all := u.notificationOf(owner, upper); class == "" {
			hint := ""
			if o.Name == "notification" {
				hint = "For every notification, write \"on(what: int) { ... }\"."
			} else if base, _ := u.virtualOwner(owner, "_"+o.Name); base != "" {
				hint = fmt.Sprintf("%s isn't a notification, but a virtual function of %s: write \"@override func _%s(...)\".", o.Name, base, o.Name)
			} else if sug := suggest(upper, all...); sug != "" {
				hint = fmt.Sprintf("Did you mean %q?", strings.ToLower(sug))
			}
			return nil, u.errorAt(o.Pos, len(o.Name)+3, fmt.Sprintf("Class %s and its bases have no notification %s.", owner, o.Name), hint)
		}
	}
	switch {
	case o.Parens && !takesParam:
		return nil, u.errorAt(o.Pos, len(o.Name)+3, fmt.Sprintf("An on %s block takes no parameters.", o.Name),
			fmt.Sprintf("Write \"%s\". Only process, physics_process, internal_process, internal_physics_process, and the nameless on block, which runs at every notification, take one.", onExample(o.Name)))
	case o.Param != nil && o.ParamType == nil:
		return nil, u.errorAt(o.Param.Pos, len(o.Param.Name), fmt.Sprintf("Parameter %s needs its type, %s.", o.Param.Name, param.typ),
			fmt.Sprintf("Write \"%s: %s\".", o.Param.Name, param.typ))
	case o.ParamType != nil && (o.ParamType.Name != param.typ || len(o.ParamType.Args) > 0):
		t := o.ParamType
		return nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Parameter %s is a %s, but found %s.", o.Param.Name, param.typ, typeString(t)),
			fmt.Sprintf("Write \"%s: %s\".", o.Param.Name, param.typ))
	}
	f := &Func{Pos: o.Pos, Annotations: o.Annotations, Name: "_" + cmp.Or(o.Name, "notification"), Return: &Type{Pos: o.Pos, Name: "void"}, Body: o.Body}
	if o.Param != nil {
		f.Params = []*Param{{Pos: o.Param.Pos, Name: o.Param.Name, Type: o.ParamType}}
	}
	m, err := u.buildFunc(f, owner, false)
	if err != nil {
		return nil, err
	}
	m.hidden = "notif"
	return m, nil
}

// onNotif returns the handler that calls f, an on block, by its C++ name, bodyName(f).
func onNotif(f *funcModel) *notifModel {
	name := f.f.Name[1:]
	n := &notifModel{cond: "WHAT == NOTIFICATION_" + strings.ToUpper(name), call: bodyName(f) + "()", setter: processing[f.f.Name]}
	switch name {
	case "ready": // POST_ENTER_TREE comes right before each READY, once the children are ready, but also on each later entry.
		n.cond = "WHAT == NOTIFICATION_POST_ENTER_TREE && !is_node_ready()"
	case "notification":
		n.cond = "true"
	}
	if len(f.f.Params) > 0 {
		key := name
		if name == "notification" {
			key = "" // The nameless block.
		}
		n.call = fmt.Sprintf("%s(%s)", bodyName(f), onParams[key].value)
	}
	return n
}

// superName returns the name of the function that "super" binds for the function name, e.g. _super_ready for
// _ready.
func superName(name string) string {
	return "_super" + name
}

// argsOf returns the arguments of a, or nil if a is nil.
func argsOf(a *Annotation) []*Arg {
	if a == nil {
		return nil
	}
	return a.Args
}

// requireBase returns an error at annotation a, if set, unless the class or extern named owner is one of bases or
// extends one of them.
func (u *unit) requireBase(a *Annotation, owner, hint string, bases ...string) error {
	if a != nil && !u.extends(owner, bases...) {
		return u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotation @%s can only be used in classes that extend %s, which %s doesn't.",
			a.Name, strings.Join(bases, " or "), owner), hint)
	}
	return nil
}

// extends reports whether the class or extern named owner is one of bases or extends one of them. False if a base
// along the way isn't known.
func (u *unit) extends(owner string, bases ...string) bool {
	for name, seen := owner, 0; seen < 100; seen++ {
		s := u.symbols[name]
		switch {
		case slices.Contains(bases, name):
			return true
		case s == nil: // Also a dependency whose base isn't known.
			return false
		case s.class != nil:
			name = baseName(s.class.Extends)
		case s.extern != nil:
			name = baseName(s.extern.Extends)
		default:
			name = s.base
		}
	}
	return true // A cycle, which kindOf reports.
}

// lifecycle adds member, a ctor or dtor block, to class m: the body of its constructor or destructor, or with
// @recycle, what its pool runs when it reuses or keeps an object.
func (u *unit) lifecycle(m *classModel, member *Member) error {
	keyword, pos := member.keyword()
	list, body, slot, recycled, only := []*Annotation(nil), (*Block)(nil), &m.ctor, &m.recycleCtor, &m.recycleCtorOnly
	if member.Ctor != nil {
		list, body = member.Ctor.Annotations, member.Ctor.Body
	} else {
		list, body, slot, recycled, only = member.Dtor.Annotations, member.Dtor.Body, &m.dtor, &m.recycleDtor, &m.recycleDtorOnly
	}
	a, err := u.annotations(list, "a "+keyword+" block", "recycle")
	if err != nil {
		return err
	}
	name, n := keyword, len(keyword)
	if r := a["recycle"]; r != nil {
		if m.pool == nil {
			return u.errorAt(r.Pos, len(r.Name)+1, fmt.Sprintf("A @recycle %s only works in a @pool class, whose objects are reused.", keyword),
				fmt.Sprintf("Add @pool to class %s, or remove @recycle.", m.name))
		}
		for i, arg := range r.Args {
			switch {
			case arg.Value != `"only"`:
				return u.errorAt(arg.Pos, len(arg.Value), "Annotation @recycle takes \"only\".", "E.g. @recycle(\"only\").")
			case i > 0:
				return u.errorAt(arg.Pos, len(arg.Value), "Annotation @recycle takes \"only\" only once.", "")
			}
		}
		slot, name, n, *only = recycled, "@recycle "+keyword, len(r.Name)+1, len(r.Args) > 0
	}
	if *slot != nil {
		return u.errorAt(pos, n, fmt.Sprintf("Class %s has two %ss.", m.name, name), "")
	}
	*slot = body
	return nil
}

// sceneOf returns the res:// path of the scene that a, the @scene annotation of the class named owner, names, or ""
// without one.
func (u *unit) sceneOf(a *Annotation, owner string) (string, error) {
	if a == nil {
		return "", nil
	}
	if err := u.requireBase(a, owner, "A scene's root is a node.", "Node"); err != nil {
		return "", err
	}
	path, err := u.classPath(a, "scene", "res://bullet.tscn")
	if rest, ok := strings.CutPrefix(path, "pkg://"); ok {
		path = strings.TrimSuffix(cmp.Or(u.opts.PackagePath, "res://"), "/") + "/" + rest
	}
	return path, err
}

// checkNoDebug returns an error for a @notrace or @noprofile of member, a func, var or signal of owner, e.g. "class
// Player", if owner has no @trace or @profile to leave member out of. trace and profile tell whether it has them.
func (u *unit) checkNoDebug(member *Member, owner string, trace, profile bool) error {
	var list []*Annotation
	switch {
	case member.Func != nil:
		list = member.Func.Annotations
	case member.Var != nil:
		list = member.Var.Annotations
	case member.Signal != nil:
		list = member.Signal.Annotations
	}
	for _, a := range list {
		if a.Name == "notrace" && !trace || a.Name == "noprofile" && !profile {
			debug := strings.TrimPrefix(a.Name, "no")
			return u.errorAt(a.Pos, len(a.Name)+1, fmt.Sprintf("Annotation @%s has no effect: %s has no @%s.", a.Name, owner, debug),
				fmt.Sprintf("Remove @%s, or add @%s to %s.", a.Name, debug, owner))
		}
	}
	return nil
}

// poolOf returns the pool that a, the @pool annotation of the class named owner, declares, or nil without one.
func (u *unit) poolOf(a *Annotation, owner string) (*poolModel, error) {
	if a == nil {
		return nil, nil
	}
	if err := u.requireBase(a, owner, "A pool takes its objects out of the scene tree, so they must be nodes.", "Node"); err != nil {
		return nil, err
	}
	p, args := &poolModel{size: "0", mode: "grow"}, a.Args
	if len(args) > 0 && !isString(args[0].Value) {
		if n, err := strconv.ParseInt(args[0].Value, 10, 64); err != nil || n <= 0 {
			return nil, u.errorAt(args[0].Pos, len(args[0].Value), "A pool's size must be a positive integer.", "E.g. \"@pool(100)\".")
		}
		p.size, p.mode, args = args[0].Value, "fixed", args[1:]
	}
	for i, arg := range args {
		v, err := u.argValue(arg)
		switch {
		case err != nil:
			return nil, err
		case !isString(arg.Value) || v != "fixed" && v != "grow" || i > 0:
			return nil, u.errorAt(arg.Pos, len(arg.Value), "Annotation @pool takes a size, and \"fixed\" or \"grow\", all optional.",
				"E.g. \"@pool\", \"@pool(100)\" or \"@pool(100, \\\"grow\\\")\".")
		case p.size == "0":
			return nil, u.errorAt(arg.Pos, len(arg.Value), fmt.Sprintf("Only a pool with a size has a mode, so %q needs one.", v),
				fmt.Sprintf("E.g. \"@pool(100, \\\"%s\\\")\".", v))
		}
		p.mode = v
	}
	return p, nil
}

// bodyName is the name of the method that holds the body of a class's @deferred, @thread_safe or @onthread func f.
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
func (u *unit) buildSignal(s *Signal, owner string) (*signalModel, error) {
	a, err := u.annotations(s.Annotations, "a signal", "notrace", "trace")
	if err != nil {
		return nil, err
	}
	m := &signalModel{s: s, notrace: a["notrace"] != nil}
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
		"export_multiline", "export_placeholder", "export_range", "export_storage", "game_only", "noprofile", "notrace", "profile", "recycle", "trace"},
		sectionAnnotations...)
	kind := "a var"
	if ext {
		allowed, kind = []string{"noprofile", "profile"}, "an extern var"
	}
	a, err := u.annotations(v.Annotations, kind, allowed...)
	if err != nil {
		return nil, err
	}
	m := &varModel{v: v, onready: a["onready"] != nil, recycle: a["recycle"], notrace: a["notrace"] != nil, noprofile: a["noprofile"] != nil, gameOnly: a["game_only"] != nil, usage: "PROPERTY_USAGE_NONE", hint: "PROPERTY_HINT_NONE"}
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
	if err := u.nodeShorthand(m); err != nil {
		return nil, err
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
		if strings.HasPrefix(e.Name, "export") && !slices.Contains(sectionAnnotations, e.Name) && m.gameOnly {
			return nil, u.errorAt(e.Pos, len(e.Name)+1, fmt.Sprintf("Annotations @game_only and @%s can't be used together.", e.Name),
				"The editor would read and save the default value, since the getter and setter don't run there.")
		}
		if strings.HasPrefix(e.Name, "export") {
			if err := u.requireBase(e, owner, "Only nodes and resources are edited in the inspector.", "Node", "Resource"); err != nil {
				return nil, err
			}
		}
	}
	if r := m.recycle; r != nil && v.Property != nil && v.Init == nil {
		return nil, u.errorAt(r.Pos, len(r.Name)+1, fmt.Sprintf("A @recycle var needs an initial value, which property %s doesn't have.", v.Name),
			"Give it one, e.g. \"= 0\", or reset it in a @recycle ctor.")
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
			a, err := u.annotations(acc.Set.Annotations, "a set block", "deferred", "thread_safe")
			if err != nil {
				return nil, err
			}
			if err := u.requireBase(a["thread_safe"], owner, "call_thread_safe is a method of Node.", "Node"); err != nil {
				return nil, err
			}
			for _, name := range []string{"deferred", "thread_safe"} {
				if a[name] != nil {
					m.deferral = name
				}
			}
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
	if m.t.async != nil {
		return u.errorAt(export.Pos, len(export.Name)+1, "Async variables can't be exported.", "Neither the inspector nor scene files can hold a task.")
	}
	if m.t.weak {
		return u.errorAt(export.Pos, len(export.Name)+1, "Weak variables can't be exported.",
			fmt.Sprintf("Export the class itself, e.g. \"@export var %s: %s\".", m.v.Name, m.t.doc))
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
		a, err := u.annotations(e.Annotations, "an extern", "pool", "profile", "scene", "trace")
		if err != nil {
			return err
		}
		for _, n := range [][2]string{{"pool", "A pool takes its objects out of the scene tree, so they must be nodes."}, {"scene", "A scene's root is a node."}} {
			if b := a[n[0]]; b != nil && len(b.Args) > 0 {
				return u.errorAt(b.Args[0].Pos, len(b.Args[0].Value), fmt.Sprintf("In externs, @%s takes no arguments: the class configures it.", n[0]), "")
			} else if err := u.requireBase(b, e.Name, n[1], "Node"); err != nil {
				return err
			}
		}
		m.pool, m.scene = a["pool"] != nil, a["scene"] != nil
		if m.trace, err = u.debugOn(a["trace"], e.Name); err != nil {
			return err
		}
		if m.profile, err = u.debugOn(a["profile"], e.Name); err != nil {
			return err
		}
		names := map[string]bool{}
		for _, member := range e.Members {
			if err := u.checkNoDebug(member, "extern "+e.Name, a["trace"] != nil, a["profile"] != nil); err != nil {
				return err
			}
			var err error
			switch {
			case member.Func != nil:
				var f *funcModel
				if f, err = u.buildFunc(member.Func, e.Name, true); err == nil {
					f.trace = f.trace || m.trace && !f.notrace
					f.profile = f.profile || m.profile && !f.noprofile && f.deferral == ""
					m.funcs = append(m.funcs, f)
					err = u.unique(names, f.f.Pos, "func", f.f.Name)
				}
			case member.Var != nil:
				var v *varModel
				if v, err = u.buildVar(member.Var, e.Name, true); err == nil {
					v.profile = v.profile || m.profile && !v.noprofile
					m.vars = append(m.vars, v)
					err = u.unique(names, v.v.Pos, "var", v.v.Name, v.getter, v.setter)
				}
			case member.Signal != nil:
				var sig *signalModel
				if sig, err = u.buildSignal(member.Signal, e.Name); err == nil {
					sig.trace = sig.trace || m.trace && !sig.notrace
					m.signals = append(m.signals, sig)
					err = u.unique(names, sig.s.Pos, "signal", sig.s.Name)
				}
			default:
				keyword, pos := member.keyword()
				return u.errorAt(pos, len(keyword), fmt.Sprintf("Externs can't contain %s.", map[string]string{"decl": "decl or impl blocks",
					"ctor": "a ctor", "dtor": "a dtor", "on": "on blocks", "enum": "enums", "import": "imports", "noimport": "imports"}[keyword]),
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
				"Members share one namespace. A var x also declares get_x and set_x, a signal declares its emit function, a @virtual func _x declares x, and @pool and @scene classes declare methods for scripts, e.g. new_pooled.")
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
	a, err := u.annotations(c.Annotations, "a class", "game_only", "icon", "pool", "profile", "scene", "tool", "trace")
	if err != nil {
		return nil, err
	}
	m.gameOnly = a["game_only"] != nil
	if m.pool, err = u.poolOf(a["pool"], c.Name); err != nil {
		return nil, err
	}
	if m.scene, err = u.sceneOf(a["scene"], c.Name); err != nil {
		return nil, err
	}
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
	names[m.newName()] = true // Methods for scripts.
	for _, name := range []string{"free_pooled", "queue_free_pooled", "pool_reserve", "pool_clear"} {
		names[name] = m.pool != nil
	}
	// Enums declared in the class.
	var declared []*symbol
	for _, member := range c.Members {
		if err := u.checkNoDebug(member, "class "+c.Name, a["trace"] != nil, a["profile"] != nil); err != nil {
			return nil, err
		}
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
		case member.Ctor != nil || member.Dtor != nil:
			if err := u.lifecycle(m, member); err != nil {
				return nil, err
			}
		case member.Func != nil && member.Func.Name == "_notification":
			return nil, u.errorAt(member.Func.Pos, 4, "Classes can't declare _notification, since GD++ generates it.",
				"Handle notifications with on blocks, e.g. \"on ready { ... }\", or \"on(what: int) { ... }\" for every notification.")
		case member.Func != nil || member.On != nil:
			var f *funcModel
			if member.On != nil {
				f, err = u.buildOn(member.On, c.Name)
			} else if f, err = u.buildFunc(member.Func, c.Name, false); err == nil {
				err = u.setVirtualOf(f, c.Name, m.base)
			}
			if err == nil && f.recycle != nil {
				r, what := f.recycle, "_ready"
				if member.On != nil {
					what = "on ready block"
				}
				switch {
				case member.Func != nil && (f.f.Name != "_ready" || !f.override || !u.extends(c.Name, "Node")):
					err = u.errorAt(r.Pos, len(r.Name)+1, "Of all functions, only an @override _ready takes @recycle.",
						"With @recycle, an on ready block, or an @override _ready, runs each time a @pool class's object gets ready, not only the first time.")
				case m.pool == nil:
					err = u.errorAt(r.Pos, len(r.Name)+1, fmt.Sprintf("A @recycle %s only works in a @pool class, whose objects are reused.", what),
						fmt.Sprintf("Add @pool to class %s, or remove @recycle.", m.name))
				}
			}
			if err == nil {
				f.once = m.pool != nil && f.f.Name == "_ready" && (f.override || f.hidden == "notif") && f.recycle == nil
				m.readyOnce = m.readyOnce || f.once
			}
			if on := member.On; err == nil && on != nil && slices.ContainsFunc(m.funcs, func(g *funcModel) bool { return g.f.Name == bodyName(f) }) {
				if on.Name == "" {
					err = u.errorAt(on.Pos, 2, fmt.Sprintf("Class %s has two nameless on blocks.", m.name), "Merge them into one.")
				} else {
					err = u.errorAt(on.Pos, len(on.Name)+3, fmt.Sprintf("Class %s has two on %s blocks.", m.name, on.Name), "Merge them into one.")
				}
			}
			if err == nil && member.On == nil { // An on block's function has a hidden name, so it can't clash.
				keyword, _ := member.keyword()
				err = u.unique(names, f.f.Pos, keyword, f.f.Name)
			}
			switch {
			case err != nil:
			case f.hidden == "notif":
				// It's a method with another name, since godot-cpp would make a method of its name an override.
				if f.f.Name == "_ready" { // Runs first, right after the @onready initializers.
					n := onNotif(f)
					if f.once {
						n.cond += " && !_gdpp_pool_slot.readied"
					}
					m.notifs = slices.Insert(m.notifs, 0, n)
				} else {
					m.notifs = append(m.notifs, onNotif(f))
				}
				body := *f.f
				body.Name = bodyName(f)
				f.f = &body
				m.funcs = append(m.funcs, f)
			case f.super:
				// The bound function has the body, so scripts can call it in place of super.
				body := *f.f
				body.Name, body.Annotations = superName(f.f.Name), nil
				f.calls = &funcModel{f: &body, params: f.params, ret: f.ret, isConst: f.isConst, gameOnly: f.gameOnly}
				m.funcs = append(m.funcs, f, f.calls)
				err = u.unique(names, f.f.Pos, "func", body.Name)
			default:
				m.funcs = append(m.funcs, f)
			}
			if err == nil && f.virtual && !f.private {
				// GDVIRTUAL_BIND doesn't make the function callable, so scripts call it through this one.
				caller := *f.f
				caller.Name, caller.Annotations = f.f.Name[1:], nil
				m.funcs = append(m.funcs, &funcModel{f: &caller, params: f.params, ret: f.ret, isConst: f.isConst, calls: f, gameOnly: f.gameOnly})
				err = u.unique(names, f.f.Pos, "func", caller.Name)
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
				m.funcs = append(m.funcs, &funcModel{f: &body, params: f.params, ret: f.ret, isConst: f.isConst, static: f.static,
					hidden: f.deferral, trace: f.trace || m.trace && !f.notrace, profile: f.profile || m.profile && !f.noprofile, gameOnly: f.gameOnly})
				f.trace, f.profile = false, false
				if f.deferral == "onthread" && !f.detached {
					f.ret = u.async(f.ret) // Callers get an Async of the body's result.
				}
				err = u.unique(names, f.f.Pos, "func", body.Name)
			}
		case member.Var != nil:
			var v *varModel
			if v, err = u.buildVar(member.Var, c.Name, false); err == nil && v.recycle != nil && m.pool == nil {
				err = u.errorAt(v.recycle.Pos, len(v.recycle.Name)+1, "A @recycle var only works in a @pool class, whose objects are reused.",
					fmt.Sprintf("Add @pool to class %s, or remove @recycle.", m.name))
			}
			if err == nil {
				m.readyOnce = m.readyOnce || m.pool != nil && v.onready && v.v.Init != nil && v.recycle == nil
				v.trace = v.trace || m.trace && !v.notrace
				v.profile = v.profile || m.profile && !v.noprofile && v.v.Property != nil
				v.gameOnly = v.gameOnly || m.gameOnly
				m.vars = append(m.vars, v)
				err = u.unique(names, v.v.Pos, "var", v.v.Name, v.getter, v.setter)
				if err == nil && v.deferral != "" {
					// The deferred call runs the set block, a separate method, which @profile and @trace's watch follow.
					p := *v.set.Param
					p.Type = v.v.Type
					body := &Func{Pos: v.set.Pos, Name: "_gdpp_body_" + v.setter, Params: []*Param{&p}, Body: v.set.Body}
					m.funcs = append(m.funcs, &funcModel{f: body, params: []*gtype{v.t}, ret: &gtype{cpp: "void", doc: "void", void: true},
						hidden: v.deferral, notrace: true, noprofile: true, profile: v.profile, gameOnly: v.gameOnly})
					err = u.unique(names, v.v.Pos, "var", body.Name)
				}
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
			if sig, err = u.buildSignal(member.Signal, c.Name); err == nil {
				sig.trace = sig.trace || m.trace && !sig.notrace
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
	// The class's @trace leaves out the functions called every frame, which would flood the output. Those are
	// always in the implicit group named after them without the underscore, process or physics_process.
	for _, f := range m.funcs {
		f.gameOnly = f.gameOnly || m.gameOnly
		if f.deferral == "" {
			name := strings.TrimPrefix(f.f.Name, "_gdpp_body_")
			perFrame := (f.override || f.hidden == "notif") && processing[name] != ""
			f.trace = f.trace || !f.notrace && (m.trace && !perFrame || perFrame && slices.Contains(u.opts.Trace, name[1:]))
			f.profile = f.profile || !f.noprofile && (m.profile || perFrame && slices.Contains(u.opts.Profile, name[1:]))
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
