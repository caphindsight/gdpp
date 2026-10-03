package syntax_0

import "github.com/alecthomas/participle/v2/lexer"

// Every AST struct has a Pos field: the position of its first token, filename included.
// For declarations with a Doc or Annotations, Pos is the position of the keyword (func, var, ...) instead.
// Later stages use Pos to emit #line directives.

// File is a whole .gd++ file. At most one of FileClass, FileExtern and FileEnum is set.
// Inline classes and externs are always listed in File, even when they appear after class_name.
type File struct {
	Pos           lexer.Position
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
	Ctor     *Ctor   `parser:"| @@"`
	Dtor     *Dtor   `parser:"| @@"`
	Notif    *Notif  `parser:"| @@"`
	Func     *Func   `parser:"| @@"`
	Signal   *Signal `parser:"| @@"`
	Var      *Var    `parser:"| @@"`
	Enum     *Enum   `parser:"| @@"`
	Import   *Type   `parser:"| 'import' @@"`
	NoImport *Type   `parser:"| 'noimport' @@ )"`
}

// Ctor is a ctor block: the constructor's body, or with @recycle, what runs when a pool reuses an object.
type Ctor struct {
	Pos         lexer.Position
	Annotations []*Annotation `parser:"( (?= '@') @@ )*"`
	Body        *Block        `parser:"'ctor' @@"`
}

// Dtor is a dtor block: the destructor's body, or with @recycle, what runs when a pool keeps an object.
type Dtor struct {
	Pos         lexer.Position
	Annotations []*Annotation `parser:"( (?= '@') @@ )*"`
	Body        *Block        `parser:"'dtor' @@"`
}

// Notif is a notif block: Body runs when the object gets one of the notifications Names (without NOTIFICATION_).
type Notif struct {
	Pos   lexer.Position
	Names []*NotifName `parser:"'notif' ( '(' @@ ( ',' @@ )* ','? ')' | @@ ( ',' @@ )* )"`
	Body  *Block       `parser:"@@"`
}

type NotifName struct {
	Pos  lexer.Position
	Name string `parser:"@Ident"`
}

// Code is an embedded C++ block: decl, impl or decl impl. It takes annotations, but no doc comment.
type Code struct {
	Pos         lexer.Position
	Annotations []*Annotation `parser:"( (?= '@') @@ )*"`
	Decl        bool          `parser:"( @'decl'"`
	Impl        bool          `parser:"  @'impl'? | @'impl' )"`
	Body        *Block        `parser:"@@"`
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
	Name    string   `parser:"@Ident"`
	Type    *Type    `parser:"( ':' @@ )?"`
	Default *Default `parser:"( '=' @@ )?"`
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
	Pos         lexer.Position
	Annotations []*Annotation `parser:"@@*"`
	Param       *Param        `parser:"'set' '(' @@ ')'"`
	Body        *Block        `parser:"@@"`
}

// Enum is an integer constant (enum NAME = 42, Value set) or an enum type (Entries, after the values of Extends),
// declared with enum_name (the rest of the file) or inline with enum Name { ... }.
type Enum struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"'enum' @Ident"`
	Value       *Int          `parser:"( '=' @@"`
	Extends     *EnumBase     `parser:"| '{' ( 'extends' @@ )?"`
	Entries     []*EnumEntry  `parser:"  @@* '}' )"`
}

// EnumBase is the enum that an enum extends: a GD++ enum, or an engine enum, e.g. Node.ProcessMode.
type EnumBase struct {
	Pos  lexer.Position
	Name string `parser:"@Ident ( @'.' @Ident )?"`
}

type EnumEntry struct {
	Pos         lexer.Position
	Doc         *Doc          `parser:"@@?"`
	Annotations []*Annotation `parser:"@@*"`
	Name        string        `parser:"@Ident"`
	Value       *EnumExpr     `parser:"( '=' @@ )? ','?"`
}

// EnumExpr is an enum value's expression: an integer (Int), a value (Ref), Op Right with Op "~" or "-", or
// Left Op Right with Op "|", "^", "&", "+", "-", "*", "/" or "%". Parentheses only group, so they leave no trace.
// Parse builds it, with C's precedence.
type EnumExpr struct {
	Pos         lexer.Position
	Int         *Int
	Ref         *EnumRef
	Op          string
	Left, Right *EnumExpr
}

// EnumRef is a value of the enum itself, e.g. HEARTS, or of another enum, e.g. Suit.HEARTS or
// GeometryInstance3D.ShadowCastingSetting.ON.
type EnumRef struct {
	Pos  lexer.Position
	Name string
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

// Init is the initial value of a variable: a one-line C++ expression (Expr) or a C++ block that returns the value.
// Pos is the position of the expression's first token.
type Init struct {
	Pos   lexer.Position
	Expr  string
	Block *Block
}

// Default is the default value of a parameter: like Init, but its expression also ends at a "," outside brackets.
type Default Init

// Doc is a documentation comment (/// lines or /** */), with the comment markers removed.
type Doc struct {
	Pos  lexer.Position
	Text string
}
