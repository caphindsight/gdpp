package syntax_0

import (
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/alecthomas/participle/v2/lexer"

	"gd++/trans/meta"
)

// Expand returns the GD++ source src with its invocations of macros and templates expanded: the source that the
// later stages parse.
func Expand(filename, src string, opts meta.Options) (string, error) {
	file, err := Parse(filename, src)
	if err != nil {
		return "", err
	}
	x, err := newExpander(filename, src, file, opts)
	if err != nil {
		return "", err
	}
	if err := x.expandFile(file); err != nil {
		return "", err
	}
	text, _ := x.splice()
	return text, nil
}

// edit replaces src[start:end], an invocation, with what it generated: items, as GD++, or text, as C++.
type edit struct {
	start, end int
	inv        lexer.Position // Where the invocation is, for an invocation of declarations.
	items      []*topItem
	text       string
}

// addDeclEdit records what the invocation inv, of declarations, generated.
func (x *expander) addDeclEdit(inv *Invoke, items []*topItem) {
	start := inv.Pos.Offset
	if inv.Doc != nil {
		start = inv.Doc.Pos.Offset
	}
	x.edits = append(x.edits, edit{start: start, end: inv.EndPos.Offset, inv: inv.Pos, items: items})
}

// region is a part of the expanded source: from the expanded file at orig, or what an invocation generated.
type region struct {
	start, end int // In the expanded source.
	orig       int
	generated  bool           // Whether an invocation generated it.
	decls      bool           // Whether it's declarations, rather than C++ code in the file's own code.
	inv        lexer.Position // For generated regions: the invocation.
}

// splice applies the edits to the expanded file's source, and returns the result, with where its parts come from.
func (x *expander) splice() (string, *expansion) {
	sort.Slice(x.edits, func(i, j int) bool { return x.edits[i].start < x.edits[j].start })
	p := &printer{blocks: map[int]Origin{}, inits: map[int]Origin{}}
	ex := &expansion{x: x, printer: p}
	last := 0
	for _, e := range x.edits {
		ex.add(region{orig: last}, x.src[last:e.start])
		if e.items == nil {
			ex.add(region{generated: true, inv: x.posAt(e.start)}, e.text)
		} else {
			lineStart := strings.LastIndexByte(x.src[:e.start], '\n') + 1
			p.indent = x.src[lineStart : lineStart+len(x.src[lineStart:e.start])-len(strings.TrimLeft(x.src[lineStart:e.start], " \t"))]
			p.sb = &ex.sb
			start := ex.sb.Len()
			p.items(e.items)
			ex.regions = append(ex.regions, region{start: start, end: ex.sb.Len(), generated: true, decls: true, inv: e.inv})
		}
		last = e.end
	}
	ex.add(region{orig: last}, x.src[last:])
	return ex.sb.String(), ex
}

// expansion is the expanded source, as splice builds it.
type expansion struct {
	x       *expander
	sb      strings.Builder
	regions []region
	printer *printer
}

// add appends text, from r.
func (ex *expansion) add(r region, text string) {
	r.start = ex.sb.Len()
	ex.sb.WriteString(text)
	r.end = ex.sb.Len()
	ex.regions = append(ex.regions, r)
}

// at returns the region of the expanded source at offset.
func (ex *expansion) at(offset int) region {
	i := sort.Search(len(ex.regions), func(i int) bool { return ex.regions[i].end > offset })
	if i == len(ex.regions) {
		i--
	}
	return ex.regions[i]
}

// pos maps p, a position in the expanded source, to the expanded file: the invocation, for generated code.
func (ex *expansion) pos(p lexer.Position) lexer.Position {
	r := ex.at(p.Offset)
	if r.generated {
		return r.inv
	}
	return ex.x.posAt(r.orig + min(p.Offset, r.end) - r.start)
}

// posAt returns the position of offset in the expanded file.
func (x *expander) posAt(offset int) lexer.Position {
	if x.lineStarts == nil {
		x.lineStarts = []int{0}
		for i, c := range x.src {
			if c == '\n' {
				x.lineStarts = append(x.lineStarts, i+1)
			}
		}
	}
	line := sort.SearchInts(x.lineStarts, offset+1) // The lines that start at offset or before it.
	return lexer.Position{Filename: x.filename, Offset: offset, Line: line, Column: utf8.RuneCountInString(x.src[x.lineStarts[line-1]:offset]) + 1}
}

// reparse parses the expanded source, which replaces f, with the positions of the expanded file. Generated code
// keeps what the writer needs to know about it: whether a macro generated it, and where a template's code comes from.
func (x *expander) reparse() (*File, error) {
	text, ex := x.splice()
	f, err := Parse(x.filename, text)
	if err != nil {
		if e, ok := err.(*Error); ok {
			e.Pos = ex.pos(e.Pos)
			if r := ex.at(e.Pos.Offset); r.generated {
				e.Stack = []string{x.generated[r.inv.Offset]}
			}
			return nil, e.withSource(x.src)
		}
		return nil, err
	}
	forEachNode(f, func(n any) {
		switch n := n.(type) {
		case *Block:
			if ex.at(n.Pos.Offset).decls {
				n.Generated, n.Origin = true, ex.printer.blocks[n.Pos.Offset]
			}
			n.TextPos = ex.pos(n.TextPos)
		case *Init:
			if ex.at(n.Pos.Offset).decls {
				n.Generated, n.Origin = true, ex.printer.inits[n.Pos.Offset]
			}
		case *Default:
			if ex.at(n.Pos.Offset).decls {
				n.Generated, n.Origin = true, ex.printer.inits[n.Pos.Offset]
			}
		}
		if p := reflectPos(n); p != nil {
			*p = ex.pos(*p)
		}
	})
	return f, nil
}

// printer writes generated declarations as GD++, exactly as the parser reads them back.
type printer struct {
	sb     *strings.Builder
	indent string         // The invocation's line's.
	depth  int            // How deeply the current line is nested.
	blocks map[int]Origin // Where the C++ code of templates comes from, by the offset of its "{".
	inits  map[int]Origin // The same for initial and default values, by the offset of their first character.
}

func (p *printer) write(ss ...string) {
	for _, s := range ss {
		p.sb.WriteString(s)
	}
}

// nl starts a new line, at the current depth.
func (p *printer) nl() {
	p.write("\n", p.indent, strings.Repeat("  ", p.depth))
}

func (p *printer) items(items []*topItem) {
	for i, item := range items {
		if i > 0 {
			p.nl()
		}
		switch {
		case item.Class != nil:
			c := item.Class
			p.body("class", c.Doc, c.Annotations, c.Name, c.Extends, c.Members)
		case item.Extern != nil:
			e := item.Extern
			p.body("extern", e.Doc, e.Annotations, e.Name, e.Extends, e.Members)
		default:
			p.member(item.Member)
		}
	}
}

// body writes a class or an extern.
func (p *printer) body(keyword string, doc *Doc, annotations []*Annotation, name string, extends *Type, members []*Member) {
	p.head(doc, annotations)
	p.write(keyword, " ", name, " {")
	p.depth++
	if extends != nil {
		p.nl()
		p.write("extends ", typeText(extends))
	}
	for _, m := range members {
		p.nl()
		p.member(m)
	}
	p.depth--
	p.nl()
	p.write("}")
}

// head writes a declaration's doc comment, on lines of its own, and its annotations.
func (p *printer) head(doc *Doc, annotations []*Annotation) {
	if doc != nil {
		for _, line := range strings.Split(doc.Text, "\n") {
			p.write(strings.TrimRight("/// "+line, " "))
			p.nl()
		}
	}
	for _, a := range annotations {
		p.write(a.label())
		if len(a.Args) > 0 {
			var args []string
			for _, arg := range a.Args {
				args = append(args, arg.Value)
			}
			p.write("(", strings.Join(args, ", "), ")")
		}
		p.write(" ")
	}
}

func (p *printer) member(m *Member) {
	switch {
	case m.Code != nil:
		p.head(nil, m.Code.Annotations)
		p.write(map[[2]bool]string{{true, false}: "decl", {false, true}: "impl", {true, true}: "decl impl"}[[2]bool{m.Code.Decl, m.Code.Impl}], " ")
		p.block(m.Code.Body)
	case m.Ctor != nil:
		p.head(nil, m.Ctor.Annotations)
		p.write("ctor ")
		p.block(m.Ctor.Body)
	case m.Dtor != nil:
		p.head(nil, m.Dtor.Annotations)
		p.write("dtor ")
		p.block(m.Dtor.Body)
	case m.On != nil:
		o := m.On
		p.head(nil, o.Annotations)
		p.write("on")
		if o.Name != "" {
			p.write(" ", o.Name)
		}
		if o.Parens {
			p.write("(")
			if o.Param != nil {
				p.write(o.Param.Name)
				if o.ParamType != nil {
					p.write(": ", typeText(o.ParamType))
				}
			}
			p.write(")")
		}
		p.write(" ")
		p.block(o.Body)
	case m.Func != nil:
		f := m.Func
		p.head(f.Doc, f.Annotations)
		p.write("func ", f.Name, "(")
		p.params(f.Params)
		p.write(")")
		if f.Return != nil {
			p.write(" -> ", typeText(f.Return))
		}
		if f.Body != nil {
			p.write(" ")
			p.block(f.Body)
		}
	case m.Signal != nil:
		s := m.Signal
		p.head(s.Doc, s.Annotations)
		p.write("signal ", s.Name)
		if s.Params != nil {
			p.write("(")
			p.params(s.Params)
			p.write(")")
		}
	case m.Var != nil:
		p.variable(m.Var)
	case m.Enum != nil:
		p.enum(m.Enum)
	case m.Import != nil:
		p.write("import ", typeText(m.Import))
	case m.NoImport != nil:
		p.write("noimport ", typeText(m.NoImport))
	}
}

func (p *printer) variable(v *Var) {
	p.head(v.Doc, v.Annotations)
	p.write("var ", v.Name)
	if v.Type != nil {
		p.write(": ", typeText(v.Type))
	}
	if v.Init != nil {
		p.write(" = ")
		p.init((*Default)(v.Init))
	}
	if v.Property == nil {
		return
	}
	p.write(" {")
	p.depth++
	for _, a := range v.Property.Accessors {
		p.nl()
		switch {
		case a.Decl != nil:
			p.write("decl ")
			p.block(a.Decl)
		case a.Get != nil:
			p.write("get ")
			p.block(a.Get)
		default:
			p.head(nil, a.Set.Annotations)
			p.write("set(")
			p.params([]*Param{a.Set.Param})
			p.write(") ")
			p.block(a.Set.Body)
		}
	}
	p.depth--
	p.nl()
	p.write("}")
}

func (p *printer) enum(e *Enum) {
	p.head(e.Doc, e.Annotations)
	p.write("enum ", e.Name)
	if e.Value != nil {
		p.write(" = ", e.Value.Raw)
		return
	}
	p.write(" {")
	p.depth++
	if e.Extends != nil {
		p.nl()
		p.write("extends ", e.Extends.Name)
	}
	for _, entry := range e.Entries {
		p.nl()
		p.head(entry.Doc, entry.Annotations)
		p.write(entry.Name)
		if entry.Value != nil {
			p.write(" = ", enumExprText(entry.Value))
		}
		p.write(",")
	}
	p.depth--
	p.nl()
	p.write("}")
}

func (p *printer) params(params []*Param) {
	for i, param := range params {
		if i > 0 {
			p.write(", ")
		}
		p.write(param.Name)
		if param.Type != nil {
			p.write(": ", typeText(param.Type))
		}
		if param.Default != nil {
			p.write(" = ")
			p.init(param.Default)
		}
	}
}

// block writes a C++ block, with its text as it is, so that its lines stay where they were.
func (p *printer) block(b *Block) {
	if b.Origin.Source != "" {
		p.blocks[p.sb.Len()] = b.Origin
	}
	p.write("{", b.Text, "}")
}

// init writes an initial or default value. A one-line expression that a macro generated stays on one line.
func (p *printer) init(d *Default) {
	if d.Origin.Source != "" {
		p.inits[p.sb.Len()] = d.Origin
	}
	switch {
	case d.Block != nil:
		p.block(d.Block)
	case d.Origin.Source == "":
		p.write(strings.ReplaceAll(d.Expr, "\n", " "))
	default:
		p.write(d.Expr)
	}
}

// typeText returns t as GD++, e.g. Array[int].
func typeText(t *Type) string {
	if len(t.Args) == 0 {
		return t.Name
	}
	var args []string
	for _, a := range t.Args {
		args = append(args, typeText(a))
	}
	return t.Name + "[" + strings.Join(args, ", ") + "]"
}

// enumExprText returns e as GD++, with parentheses around each operation inside it.
func enumExprText(e *EnumExpr) string {
	sub := func(e *EnumExpr) string {
		if e.Op != "" {
			return "(" + enumExprText(e) + ")"
		}
		return enumExprText(e)
	}
	switch {
	case e.Int != nil:
		return e.Int.Raw
	case e.Ref != nil:
		return e.Ref.Name
	case e.Left == nil:
		return e.Op + sub(e.Right)
	}
	return sub(e.Left) + " " + e.Op + " " + sub(e.Right)
}
