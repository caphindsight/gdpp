package syntax_1

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
	Enum   *enumHead   `parser:"| @@ )?"`
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

type topItem struct {
	Pos    lexer.Position
	Class  *Class  `parser:"( @@"`
	Extern *Extern `parser:"| @@"`
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
	}
	for _, item := range pf.Items {
		m := item.Member
		switch {
		case item.Class != nil:
			f.InlineClasses = append(f.InlineClasses, item.Class)
		case item.Extern != nil:
			f.InlineExterns = append(f.InlineExterns, item.Extern)
		case members != nil:
			*members = append(*members, m)
		case m.Enum != nil && m.Enum.Value == nil:
			f.InlineEnums = append(f.InlineEnums, m.Enum)
		default:
			return nil, outsideClass(m)
		}
	}
	return f, nil
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
	}
	if !m.Code.Decl {
		return "impl", m.Code.Pos
	}
	return "decl", m.Code.Pos
}
