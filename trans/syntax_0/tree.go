package syntax_0

import (
	"fmt"

	"github.com/alecthomas/participle/v2/lexer"
)

// The parse tree differs from the AST only at the top of the file, where file-level and inline classes
// have different syntax but the same AST node. Parse converts it with toFile.

type parsedFile struct {
	Pos    lexer.Position
	Code   []*Code     `parser:"@@*"`
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
	Entries     []*EnumEntry  `parser:"@@*"`
}

type topItem struct {
	Pos    lexer.Position
	Class  *Class  `parser:"( @@"`
	Extern *Extern `parser:"| @@"`
	Member *Member `parser:"| @@ )"`
}

// toFile converts the parse tree into a File. After class_name or extern_name, members go into that class;
// in a file without either, only decl, impl and enums are allowed outside inline classes.
func (pf *parsedFile) toFile() (*File, *Error) {
	f := &File{Pos: pf.Pos, Code: pf.Code}
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
		f.FileEnum = &Enum{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Name: h.Name, Entries: h.Entries}
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
		case m.Code != nil:
			f.Code = append(f.Code, m.Code)
		case m.Enum != nil:
			f.InlineEnums = append(f.InlineEnums, m.Enum)
		default:
			keyword, pos := m.keyword()
			return nil, &Error{Pos: pos, Len: len(keyword), Msg: fmt.Sprintf("This %s is outside of any class.", keyword),
				Hint: "Add \"class_name Name\" at the top of the file to make the whole file a class, or move this into an inline class: \"class Name { ... }\"."}
		}
	}
	return f, nil
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
	case m.Import != nil:
		return "import", m.Pos
	case m.NoImport != nil:
		return "noimport", m.Pos
	}
	return "decl", m.Pos
}
