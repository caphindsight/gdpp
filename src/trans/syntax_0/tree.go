package syntax_0

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"
)

// The parse tree differs from the AST only at the top of the file, where file-level and inline classes
// have different syntax but the same AST node. Parse converts it with toFile.

type parsedFile struct {
	Pos    lexer.Position
	Code   []*Code     `parser:"@@*"` // Only parsed to report it: code must be in a class.
	Class  *classHead  `parser:"( @@"`
	Extern *externHead `parser:"| @@"`
	Enum   *enumHead   `parser:"| @@"`
	Macro  *macroHead  `parser:"| @@ )?"`
	Items  []*topItem  `parser:"@@*"`
}

type classHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'class_name' @Ident"`
	Extends     *Type         `parser:"( 'extends' @@ )?"`
}

type externHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'extern_name' @Ident"`
	Extends     *Type         `parser:"( 'extends' @@ )?"`
}

// enumHead is enum_name and the enum values after it: the rest of the file.
type enumHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'enum_name' @Ident"`
	Extends     *EnumBase     `parser:"( 'extends' @@ )?"`
	Entries     []*EnumEntry  `parser:"@@*"`
}

// macroHead is macro_name or template_name and its parameters. Its body is the rest of the file.
type macroHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Template    bool          `parser:"( 'macro_name' | @'template_name' )"`
	Name        string        `parser:"@Ident '('"`
	Params      []*MacroParam `parser:"( @@ ( ',' @@ )* ','? )? ')'"`
	Body        *fileBody     `parser:"@@"`
}

// fileBody is the body of a file-level macro or template: the rest of the file.
type fileBody MacroBody

type topItem struct {
	Pos    lexer.Position
	Class  *Class  `parser:"( @@"`
	Extern *Extern `parser:"| @@"`
	Macro  *Macro  `parser:"| @@"`
	Member *Member `parser:"| @@ )"`
}

// toFile converts the parse tree into a File. After class_name or extern_name, members go into that class;
// in a file without either, only enums are allowed outside inline classes.
func (pf *parsedFile) toFile() (*File, *Error) {
	if len(pf.Code) > 0 {
		return nil, outsideClass(&Member{Pos: pf.Code[0].Pos, Code: pf.Code[0]})
	}
	f := &File{Pos: pf.Pos}
	var members *[]*Member
	switch {
	case pf.Class != nil:
		h := pf.Class
		f.FileClass = &Class{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Name: h.Name, Extends: h.Extends}
		members = &f.FileClass.Members
	case pf.Extern != nil:
		h := pf.Extern
		f.FileExtern = &Extern{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Name: h.Name, Extends: h.Extends}
		members = &f.FileExtern.Members
	case pf.Enum != nil:
		h := pf.Enum
		f.FileEnum = &Enum{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Name: h.Name, Extends: h.Extends, Entries: h.Entries}
	case pf.Macro != nil:
		h := pf.Macro
		f.FileMacro = &Macro{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Template: h.Template, Name: h.Name, Params: h.Params,
			Body: (*MacroBody)(h.Body)}
	}
	for _, item := range pf.Items {
		m := item.Member
		switch {
		case item.Class != nil:
			f.InlineClasses = append(f.InlineClasses, item.Class)
		case item.Extern != nil:
			f.InlineExterns = append(f.InlineExterns, item.Extern)
		case item.Macro != nil:
			f.InlineMacros = append(f.InlineMacros, item.Macro)
		case members != nil:
			*members = append(*members, m)
		case m.Enum != nil && m.Enum.Value == nil:
			f.InlineEnums = append(f.InlineEnums, m.Enum)
		case m.Invoke != nil:
			f.Invokes = append(f.Invokes, m.Invoke)
		default:
			return nil, outsideClass(m)
		}
	}
	macros := f.InlineMacros
	if f.FileMacro != nil {
		macros = append([]*Macro{f.FileMacro}, macros...)
	}
	for _, m := range macros {
		if e := m.check(); e != nil {
			return nil, e
		}
		f.InlineMacros = append(f.InlineMacros, m.helpers()...)
	}
	return f, nil
}

// check reports a doc comment or annotations on m: Godot never sees a macro, so they'd have no effect.
func (m *Macro) check() *Error {
	what := m.what()
	switch {
	case m.Doc != nil:
		return &Error{Pos: m.Doc.Pos, Len: 3, Msg: fmt.Sprintf("%ss have no doc comments, since Godot never sees them.", capitalize(what)),
			Hint: "Use a plain comment: \"//\" or \"/* */\"."}
	case len(m.Annotations) > 0:
		a := m.Annotations[0]
		return &Error{Pos: a.Pos, Len: len(a.Name) + 1, Msg: fmt.Sprintf("%ss take no annotations.", capitalize(what))}
	}
	return nil
}

// what returns "macro" or "template".
func (m *Macro) what() string {
	if m.Template {
		return "template"
	}
	return "macro"
}

// helpers moves the macros and templates declared in m's body, and in theirs, and so on, out of their bodies, and
// returns them.
func (m *Macro) helpers() []*Macro {
	var all []*Macro
	for _, h := range m.Body.Helpers {
		all = append(append(all, h), h.helpers()...)
	}
	m.Body.Helpers = nil
	return all
}

// outsideClass returns the error for member m outside of any class.
func outsideClass(m *Member) *Error {
	keyword, pos := m.keyword()
	hint := "Add \"class_name Name\" at the top of the file to make the whole file a class, or move this into an inline class: \"class Name { ... }\"."
	if m.Code != nil {
		hint = "Move this into a class. To put its code outside the class and the godot namespace, add @global: \"@global " + keyword + " { ... }\"."
	}
	return &Error{Pos: pos, Len: len(keyword), Msg: fmt.Sprintf("This %s is outside of any class.", keyword), Hint: hint}
}

// keyword returns the keyword that starts m, and its position.
func (m *Member) keyword() (string, lexer.Position) {
	switch {
	case m.Func != nil:
		return "func", m.Func.Pos
	case m.Signal != nil:
		return "signal", m.Signal.Pos
	case m.Var != nil:
		return "var", m.Var.Pos
	case m.Enum != nil:
		return "enum", m.Enum.Pos
	case m.Ctor != nil:
		return "ctor", m.Pos
	case m.Dtor != nil:
		return "dtor", m.Pos
	case m.On != nil:
		return "on", m.On.Pos
	case m.Import != nil:
		return "import", m.Pos
	case m.NoImport != nil:
		return "noimport", m.Pos
	case m.Invoke != nil:
		return "invoke", m.Invoke.Pos
	}
	if !m.Code.Decl {
		return "impl", m.Code.Pos
	}
	return "decl", m.Code.Pos
}
