package syntax_0

import "github.com/alecthomas/participle/v2/lexer"

// Every AST struct has a Pos field: the position of its first token, filename included.
// For declarations with a Doc or Annotations, Pos is the position of the keyword (func, var, ...) instead.
// Later stages use Pos to emit #line directives.

// File is a whole .gd++ file. At most one of FileClass, FileExtern and FileEnum is set.
// Inline classes and externs are always listed in File, even when they appear after class_name.
type File struct {
	Pos           lexer.Position
	Code          []*Code   // decl and impl blocks outside any class: before class_name, or in a file without one.
	FileClass     *Class    // Declared with class_name.
	InlineClasses []*Class  // Declared with class Name { ... }.
	FileExtern    *Extern   // Declared with extern_name.
	InlineExterns []*Extern // Declared with extern Name { ... }.
	FileEnum      *Enum     // Declared with enum_name.
	InlineEnums   []*Enum   // Enums in a file without a file-level class. (Otherwise they are class members.)
}

// Member is one declaration in a class or extern. Exactly one field after Pos is set.
type Member struct {
	Pos      lexer.Position
	Code     *Code   `parser:"( @@"`
	Ctor     *Block  `parser:"| 'ctor' @@"`
	Dtor     *Block  `parser:"| 'dtor' @@"`
	Func     *Func   `parser:"| @@"`
	Signal   *Signal `parser:"| @@"`
	Var      *Var    `parser:"| @@"`
	Enum     *Enum   `parser:"| @@"`
	Import   *Type   `parser:"| 'import' @@"`
	NoImport *Type   `parser:"| 'noimport' @@ )"`
}

// Code is an embedded C++ block: decl, impl or decl impl.
type Code struct {
	Pos  lexer.Position
	Decl bool   `parser:"( @'decl'"`
	Impl bool   `parser:"  @'impl'? | @'impl' )"`
	Body *Block `parser:"@@"`
}

// Class is a class, declared with class_name (the rest of the file) or inline with class Name { ... }.
type Class struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'class' @Ident '{'"`
	Extends     *Type         `parser:"( 'extends' @@ )?"`
	Members     []*Member     `parser:"@@* '}'"`
}

// Extern is an extern, declared with extern_name (the rest of the file) or inline with extern Name { ... }.
type Extern struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'extern' @Ident '{'"`
	Extends     *Type         `parser:"( 'extends' @@ )?"`
	Members     []*Member     `parser:"@@* '}'"`
}

// Func is a function. Without a Return type it returns a Variant; without a Body GD++ generates one.
type Func struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'func' @Ident '('"`
	Params      []*Param      `parser:"( @@ ( ',' @@ )* ','? )? ')'"`
	Return      *Type         `parser:"( '->' @@ )?"`
	Body        *Block        `parser:"@@?"`
}

// Param is a function, signal or setter parameter. Without a Type it is a Variant.
// Only function parameters may have a Default.
type Param struct {
	Pos     lexer.Position
	Name    string `parser:"@Ident"`
	Type    *Type  `parser:"( ':' @@ )?"`
	Default *Init  `parser:"( '=' @@ )?"`
}

// Type is a Godot type name, possibly with type arguments, e.g. Array[int].
type Type struct {
	Pos  lexer.Position
	Name string  `parser:"@Ident"`
	Args []*Type `parser:"( '[' @@ ( ',' @@ )* ']' )?"`
}

type Signal struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'signal' @Ident"`
	Params      []*Param      `parser:"( '(' ( @@ ( ',' @@ )* ','? )? ')' )?"`
}

// Var is a simple variable (optional Init) or a property (Property set).
type Var struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'var' @Ident"`
	Type        *Type         `parser:"( ':' @@ )?"`
	Init        *Init         `parser:"( '=' @@"`
	Property    *Property     `parser:"| @@ )?"`
}

type Property struct {
	Pos       lexer.Position
	Accessors []*Accessor `parser:"'{' @@* '}'"`
}

// Accessor is one part of a property body. Exactly one field after Pos is set.
type Accessor struct {
	Pos  lexer.Position
	Decl *Block  `parser:"( 'decl' @@"`
	Get  *Block  `parser:"| 'get' @@"`
	Set  *Setter `parser:"| @@ )"`
}

type Setter struct {
	Pos   lexer.Position
	Param *Param `parser:"'set' '(' @@ ')'"`
	Body  *Block `parser:"@@"`
}

// Enum is an integer constant (enum NAME = 42, Value set) or an enum type (Entries),
// declared with enum_name (the rest of the file) or inline with enum Name { ... }.
type Enum struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'enum' @Ident"`
	Value       *Int          `parser:"( '=' @@"`
	Entries     []*EnumEntry  `parser:"| '{' @@* '}' )"`
}

type EnumEntry struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"@Ident"`
	Value       *Int          `parser:"( '=' @@ )? ','?"`
}

// Int is an integer literal. Raw is the source text; Parse converts it into Value.
type Int struct {
	Pos   lexer.Position
	Raw   string `parser:"@( '-'? Number )" dump:"-"`
	Value int64
}

// Annotation is @Name or @Name(Args...). Attributes such as @virtual are annotations too.
// A doc comment among annotations is parsed as one with only Doc set, which Parse moves to the declaration's Doc.
type Annotation struct {
	Pos  lexer.Position
	Doc  *Doc   `parser:"( @@"`
	Name string `parser:"| '@' @Ident"`
	Args []*Arg `parser:"  ( '(' ( @@ ( ',' @@ )* ','? )? ')' )? )"`
}

// Arg is an annotation argument, kept as source text (strings keep their quotes).
type Arg struct {
	Pos   lexer.Position
	Value string `parser:"@( String | Char | '-'? Number | Ident )"`
}

// Block is a C++ block in braces. Text is the C++ code between the braces, with comments removed.
// Text keeps every newline of the source, so its first line is at TextPos.Line.
type Block struct {
	Pos     lexer.Position
	TextPos lexer.Position
	Text    string
}

// Init is the initial value of a variable or the default value of a parameter: a one-line C++ expression (Expr),
// which ends at a newline or at a "," outside brackets, or a C++ block that returns the value.
// Pos is the position of the expression's first token.
type Init struct {
	Pos   lexer.Position
	Expr  string
	Block *Block
}

// Doc is a documentation comment (/// lines or /** */), with the comment markers removed.
type Doc struct {
	Pos  lexer.Position
	Text string
}
