package syntax_1

import (
	"fmt"
	"slices"

	"github.com/alecthomas/participle/v2/lexer"
)

// The parse tree differs from the AST only at the top of the file, where file-level and inline classes
// have different syntax but the same AST node. Parse converts it with toFile.

type parsedFile struct {
	Pos    lexer.Position
	Code   []*Code     `parser:"@@*"` // Only parsed to report it: code must be in a class.
	Class  *classHead  `parser:"( @@"`
	Extern *externHead `parser:"| @@"`
	Trait  *traitHead  `parser:"| @@"`
	Struct *structHead `parser:"| @@"`
	Enum   *enumHead   `parser:"| @@"`
	Macro  *macroHead  `parser:"| @@"`
	Lib    *libHead    `parser:"| @@"`
	Shader *shaderHead `parser:"| @@ )?"`
	Items  []*topItem  `parser:"@@*"`
}

type classHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'class_name' @Ident"`
	Extends     *Type         `parser:"( 'extends' @@ )?"`
	Implements  []*Type       `parser:"( 'implements' @@ ( ',' @@ )* )*"`
}

type externHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'extern_name' @Ident"`
	Extends     *Type         `parser:"( 'extends' @@ )?"`
}

type traitHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'trait_name' @Ident"`
	Extends     *Type         `parser:"( 'extends' @@ )?"`
}

type structHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'struct_name' @Ident"`
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

// libHead is macro_library. Its body is the rest of the file.
type libHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Body        *fileBody     `parser:"'macro_library' @@"`
}

// shaderHead is shader_library. Its body is the rest of the file: GLSL that every shader of the package uses.
type shaderHead struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Body        *restBlock    `parser:"'shader_library' @@"`
}

// restBlock is a block that is the rest of the file, without braces.
type restBlock Block

func (b *restBlock) Parse(lex *lexer.PeekingLexer) error {
	first := peekRaw(lex)
	b.Pos, b.TextPos = first.Pos, first.Pos
	var code codeBuilder
	for t := nextRaw(lex); !t.EOF(); t = nextRaw(lex) {
		code.add(t)
	}
	b.Text = code.String()
	return nil
}

// fileBody is the body of a file-level macro or template: the rest of the file.
type fileBody MacroBody

type topItem struct {
	Pos        lexer.Position
	Class      *Class          `parser:"( @@"`
	Extern     *Extern         `parser:"| @@"`
	Trait      *Trait          `parser:"| @@"`
	Struct     *Struct         `parser:"| @@"`
	Macro      *Macro          `parser:"| @@"`
	Annotation *AnnotationDecl `parser:"| @@"`
	Member     *Member         `parser:"| @@ )"`
}

// toFile converts the parse tree into a File. After class_name, extern_name, trait_name or struct_name, members go
// into that class, extern, trait or struct; in a file without any, only enums are allowed outside inline classes.
func (pf *parsedFile) toFile() (*File, *Error) {
	f := &File{Pos: pf.Pos}
	for _, code := range pf.Code {
		if !code.Shader {
			return nil, outsideClass(&Member{Pos: code.Pos, Code: code})
		}
		f.ShaderBlocks = append(f.ShaderBlocks, code)
	}
	var members *[]*Member
	switch {
	case pf.Class != nil:
		h := pf.Class
		f.FileClass = &Class{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Name: h.Name, Extends: h.Extends, Implements: h.Implements}
		members = &f.FileClass.Members
	case pf.Extern != nil:
		h := pf.Extern
		f.FileExtern = &Extern{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Name: h.Name, Extends: h.Extends}
		members = &f.FileExtern.Members
	case pf.Trait != nil:
		h := pf.Trait
		f.FileTrait = &Trait{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Name: h.Name, Extends: h.Extends}
		members = &f.FileTrait.Members
	case pf.Struct != nil:
		h := pf.Struct
		f.FileStruct = &Struct{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Name: h.Name}
		members = &f.FileStruct.Members
	case pf.Enum != nil:
		h := pf.Enum
		f.FileEnum = &Enum{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Name: h.Name, Extends: h.Extends, Entries: h.Entries}
	case pf.Macro != nil:
		h := pf.Macro
		f.FileMacro = &Macro{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Template: h.Template, Name: h.Name, Params: h.Params,
			Body: (*MacroBody)(h.Body)}
	case pf.Lib != nil:
		h := pf.Lib
		f.Libraries = []*Macro{{Pos: h.Pos, Doc: h.Doc, Annotations: h.Annotations, Body: (*MacroBody)(h.Body)}}
	case pf.Shader != nil:
		h := pf.Shader
		if len(h.Annotations) > 0 {
			a := h.Annotations[0]
			return nil, &Error{Pos: a.Pos, Len: len(a.label()), Msg: fmt.Sprintf("Annotation %s can't be used on a shader library.", a.label())}
		}
		f.ShaderBlocks = append(f.ShaderBlocks, &Code{Pos: h.Pos, Shader: true, Body: (*Block)(h.Body)})
	}
	for _, item := range pf.Items {
		m := item.Member
		switch {
		case item.Class != nil:
			f.InlineClasses = append(f.InlineClasses, item.Class)
		case item.Extern != nil:
			f.InlineExterns = append(f.InlineExterns, item.Extern)
		case item.Trait != nil:
			f.InlineTraits = append(f.InlineTraits, item.Trait)
		case item.Struct != nil:
			f.InlineStructs = append(f.InlineStructs, item.Struct)
		case item.Macro != nil && item.Macro.Name == "":
			f.Libraries = append(f.Libraries, item.Macro)
		case item.Macro != nil:
			f.InlineMacros = append(f.InlineMacros, item.Macro)
		case item.Annotation != nil:
			if e := item.Annotation.check(); e != nil {
				return nil, e
			}
			f.UserAnnotations = append(f.UserAnnotations, item.Annotation)
		case members != nil:
			*members = append(*members, m)
		case m.Enum != nil && m.Enum.Value == nil:
			f.InlineEnums = append(f.InlineEnums, m.Enum)
		case m.Invoke != nil:
			f.Invokes = append(f.Invokes, m.Invoke)
		case m.Code != nil && m.Code.Shader:
			f.ShaderBlocks = append(f.ShaderBlocks, m.Code)
		default:
			return nil, outsideClass(m)
		}
	}
	macros := slices.Concat(f.Libraries, f.InlineMacros)
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

// check reports a doc comment or annotations on m: Godot never sees a macro, so they'd have no effect. It also
// reports a template without a name.
func (m *Macro) check() *Error {
	what := capitalize(m.what()) + "s"
	if m.Name == "" {
		what = "Macro libraries"
	}
	switch {
	case m.Template && m.Name == "":
		return &Error{Pos: m.Pos, Len: len("template"), Msg: "Templates need a name and parameters: template name(...) { ... }.",
			Hint: "Only macros can do without, as macro libraries: macro { ... }."}
	case m.Doc != nil:
		return &Error{Pos: m.Doc.Pos, Len: 3, Msg: fmt.Sprintf("%s have no doc comments, since Godot never sees them.", what),
			Hint: "Use a plain comment: \"//\" or \"/* */\"."}
	case len(m.Annotations) > 0:
		a := m.Annotations[0]
		return &Error{Pos: a.Pos, Len: len(a.label()), Msg: fmt.Sprintf("%s take no annotations.", what)}
	}
	return nil
}

// what returns "macro", "template" or "macro library".
func (m *Macro) what() string {
	switch {
	case m.Template:
		return "template"
	case m.Name == "":
		return "macro library"
	}
	return "macro"
}

// check reports a doc comment or annotations on d: like macros, Godot never sees it.
func (d *AnnotationDecl) check() *Error {
	switch {
	case d.Doc != nil:
		return &Error{Pos: d.Doc.Pos, Len: 3, Msg: "Annotation declarations have no doc comments, since Godot never sees them.",
			Hint: "Use a plain comment: \"//\" or \"/* */\"."}
	case len(d.Annotations) > 0:
		a := d.Annotations[0]
		return &Error{Pos: a.Pos, Len: len(a.label()), Msg: "Annotation declarations take no annotations."}
	}
	return nil
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
	case m.Shader != nil:
		return "shader", m.Shader.Pos
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
	if m.Code.Shader {
		return "shader", m.Code.Pos
	}
	if !m.Code.Decl {
		return "impl", m.Code.Pos
	}
	return "decl", m.Code.Pos
}
