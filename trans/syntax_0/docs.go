package syntax_0

import (
	"fmt"
	"slices"
	"strings"
	"unicode"
)

// docData is a doc comment, interpreted like GDScript interprets ## comments (see parse_doc_comment and
// parse_class_doc_comment in Godot's modules/gdscript/gdscript_parser.cpp).
type docData struct {
	brief, description       string
	tutorials                [][2]string // Title and link.
	deprecated, experimental *string     // Set by @deprecated and @experimental, with an optional message.
}

type docState int

const (
	docNormal docState = iota
	docInCode
	docInCodeblock
	docInKbd
)

// parseDoc interprets doc comment text. For a class, the first paragraph is the brief description, and
// @tutorial lines are allowed.
func parseDoc(text string, class bool) docData {
	var d docData
	if text == "" {
		return d
	}
	lines := strings.Split(text, "\n")
	prefix := lines[0][:len(lines[0])-len(strings.TrimLeft(lines[0], " "))]
	state := docNormal
	inBrief := class
	for _, line := range lines {
		if state == docNormal {
			stripped := strings.TrimSpace(line)
			switch {
			case inBrief && d.brief != "" && stripped == "":
				inBrief = false // A blank line ends the brief description.
				continue
			case class && strings.HasPrefix(stripped, "@tutorial"):
				if title, link, ok := parseTutorial(stripped); ok {
					d.tutorials = append(d.tutorials, [2]string{title, link})
				}
				continue
			case stripped == "@deprecated" || strings.HasPrefix(stripped, "@deprecated:"):
				msg := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(stripped, "@deprecated"), ":"))
				d.deprecated = &msg
				continue
			case stripped == "@experimental" || strings.HasPrefix(stripped, "@experimental:"):
				msg := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(stripped, "@experimental"), ":"))
				d.experimental = &msg
				continue
			}
		}
		if inBrief {
			d.brief += processDocLine(line, d.brief, prefix, &state)
		} else {
			d.description += processDocLine(line, d.description, prefix, &state)
		}
	}
	return d
}

// parseTutorial parses "@tutorial: link" or "@tutorial(Title): link".
func parseTutorial(s string) (title, link string, ok bool) {
	rest := strings.TrimPrefix(s, "@tutorial")
	if strings.HasPrefix(rest, ":") {
		return "", strings.TrimSpace(rest[1:]), true
	}
	rest = strings.TrimLeft(rest, " \t")
	if !strings.HasPrefix(rest, "(") {
		return "", "", false
	}
	title, rest, found := strings.Cut(rest[1:], ")")
	rest = strings.TrimLeft(rest, " \t")
	if !found || !strings.HasPrefix(rest, ":") {
		return "", "", false
	}
	return strings.TrimSpace(title), strings.TrimSpace(rest[1:]), true
}

// processDocLine returns line as it continues text: joined with a space, or a newline around code blocks.
// It tracks [code], [codeblock] and [kbd] tags, inside which text is kept as is. A port of _process_doc_line.
func processDocLine(line, text, prefix string, state *docState) string {
	if *state == docNormal {
		line = strings.TrimLeftFunc(line, isDocSpace)
	} else {
		line = strings.TrimPrefix(line, prefix)
	}
	join := ""
	if text != "" {
		switch {
		case *state != docNormal:
			join = "\n"
		case strings.HasSuffix(text, "[/codeblock]"):
			join = "\n"
		case !strings.HasSuffix(text, "[br]"):
			join = " "
		}
	}
	result := ""
	from, bufferStart := 0, 0
	for {
		switch *state {
		case docNormal:
			lb := indexFrom(line, "[", from)
			if lb < 0 {
				goto done
			}
			rb := indexFrom(line, "]", lb+1)
			if rb < 0 {
				goto done
			}
			from = rb + 1
			tag := line[lb+1 : rb]
			switch {
			case tag == "code" || strings.HasPrefix(tag, "code "):
				*state = docInCode
			case tag == "codeblock" || strings.HasPrefix(tag, "codeblock "):
				if lb == 0 {
					join = "\n"
				} else {
					result += line[bufferStart:lb] + "\n"
				}
				result += "[" + tag + "]"
				if from < len(line) {
					result += "\n"
				}
				*state = docInCodeblock
				bufferStart = from
			case tag == "kbd":
				*state = docInKbd
			}
		case docInCode, docInKbd:
			end := map[docState]string{docInCode: "[/code]", docInKbd: "[/kbd]"}[*state]
			pos := indexFrom(line, end, from)
			if pos < 0 {
				goto done
			}
			from = pos + len(end)
			*state = docNormal
		case docInCodeblock:
			pos := indexFrom(line, "[/codeblock]", from)
			if pos < 0 {
				goto done
			}
			from = pos + len("[/codeblock]")
			if pos == 0 {
				join = "\n"
			} else {
				result += line[bufferStart:pos] + "\n"
			}
			result += "[/codeblock]"
			if from < len(line) {
				result += "\n"
			}
			*state = docNormal
			bufferStart = from
		}
	}
done:
	result += line[bufferStart:]
	if *state == docNormal {
		result = strings.TrimRightFunc(result, isDocSpace)
	}
	return join + result
}

// isDocSpace is Godot's strip_edges test: any character up to a space.
func isDocSpace(r rune) bool {
	return r <= ' '
}

func indexFrom(s, sub string, from int) int {
	if from > len(s) {
		return -1
	}
	if i := strings.Index(s[from:], sub); i >= 0 {
		return from + i
	}
	return -1
}

func docText(d *Doc) string {
	if d == nil {
		return ""
	}
	return d.Text
}

// xmlEscape is Godot's String::xml_escape.
func xmlEscape(s string, quotes bool) string {
	s = strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
	if quotes {
		s = strings.NewReplacer("'", "&apos;", "\"", "&quot;").Replace(s)
	}
	return s
}

// naturalLess is Godot's naturalcasecmp_to(...) < 0: case-insensitive, with digit runs compared as numbers.
func naturalLess(a, b string) bool {
	ra, rb := []rune(a), []rune(b)
	i, j := 0, 0
	for i < len(ra) && j < len(rb) {
		if unicode.IsDigit(ra[i]) && unicode.IsDigit(rb[j]) {
			si, sj := i, j
			for i < len(ra) && unicode.IsDigit(ra[i]) {
				i++
			}
			for j < len(rb) && unicode.IsDigit(rb[j]) {
				j++
			}
			na, nb := strings.TrimLeft(string(ra[si:i]), "0"), strings.TrimLeft(string(rb[sj:j]), "0")
			if len(na) != len(nb) {
				return len(na) < len(nb)
			}
			if na != nb {
				return na < nb
			}
			continue
		}
		ca, cb := unicode.ToLower(ra[i]), unicode.ToLower(rb[j])
		if ca != cb {
			return ca < cb
		}
		i, j = i+1, j+1
	}
	return len(ra)-i < len(rb)-j
}

// xmlWriter writes like DocTools::save_classes: tabs, and nothing for empty strings.
type xmlWriter struct{ sb strings.Builder }

func (x *xmlWriter) ln(tabs int, s string) {
	if s != "" {
		x.sb.WriteString(strings.Repeat("\t", tabs) + s + "\n")
	}
}

// docAttrs returns the deprecated and experimental attributes of d.
func docAttrs(d docData) string {
	s := ""
	if d.deprecated != nil {
		s += fmt.Sprintf(" deprecated=\"%s\"", xmlEscape(*d.deprecated, true))
	}
	if d.experimental != nil {
		s += fmt.Sprintf(" experimental=\"%s\"", xmlEscape(*d.experimental, true))
	}
	return s
}

// typeAttrs returns the type and enum attributes of t, as class c exposes it.
func typeAttrs(c *classModel, t *gtype) string {
	s := fmt.Sprintf(" type=\"%s\"", xmlEscape(t.doc, true))
	if t.enum != nil {
		s += fmt.Sprintf(" enum=\"%s\"", xmlEscape(c.name+"."+t.enum.name, true))
		if t.enum.bitfield {
			s += " is_bitfield=\"true\""
		}
	}
	return s
}

// methodDoc is a method or signal, as _write_method_doc writes it.
type methodDoc struct {
	name, qualifiers string
	ret              *gtype // Nil for signals.
	params           []*gtype
	list             []*Param
	doc              docData
}

func (x *xmlWriter) methods(c *classModel, element string, docs []methodDoc) {
	if len(docs) == 0 {
		return
	}
	slices.SortStableFunc(docs, func(a, b methodDoc) int {
		if naturalLess(a.name, b.name) {
			return -1
		}
		if naturalLess(b.name, a.name) {
			return 1
		}
		return 0
	})
	x.ln(1, "<"+element+"s>")
	for _, m := range docs {
		attrs := ""
		if m.qualifiers != "" {
			attrs += fmt.Sprintf(" qualifiers=\"%s\"", xmlEscape(m.qualifiers, true))
		}
		x.ln(2, fmt.Sprintf("<%s name=\"%s\"%s%s>", element, xmlEscape(m.name, true), attrs, docAttrs(m.doc)))
		if m.ret != nil {
			x.ln(3, fmt.Sprintf("<return%s />", typeAttrs(c, m.ret)))
		}
		for i, t := range m.params {
			attrs := typeAttrs(c, t)
			if d := m.list[i].Default; d != nil && d.Block == nil {
				attrs += fmt.Sprintf(" default=\"%s\"", xmlEscape(d.Expr, true))
			}
			x.ln(3, fmt.Sprintf("<param index=\"%d\" name=\"%s\"%s />", i, xmlEscape(m.list[i].Name, true), attrs))
		}
		x.ln(3, "<description>")
		x.ln(4, xmlEscape(strings.TrimFunc(m.doc.description, isDocSpace), false))
		x.ln(3, "</description>")
		x.ln(2, "</"+element+">")
	}
	x.ln(1, "</"+element+"s>")
}

// document returns the Godot XML documentation of class c, in the format of DocTools::save_classes.
func (u *unit) document(c *classModel) string {
	x := &xmlWriter{}
	d := parseDoc(docText(c.cls.Doc), true)
	x.ln(0, `<?xml version="1.0" encoding="UTF-8" ?>`)
	x.ln(0, fmt.Sprintf(`<class name="%s" inherits="%s"%s xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="https://raw.githubusercontent.com/godotengine/godot/master/doc/class.xsd">`,
		xmlEscape(c.name, true), xmlEscape(c.base, true), docAttrs(d)))
	x.ln(1, "<brief_description>")
	x.ln(2, xmlEscape(strings.TrimFunc(d.brief, isDocSpace), false))
	x.ln(1, "</brief_description>")
	x.ln(1, "<description>")
	x.ln(2, xmlEscape(strings.TrimFunc(d.description, isDocSpace), false))
	x.ln(1, "</description>")
	x.ln(1, "<tutorials>")
	for _, t := range d.tutorials {
		title := ""
		if t[0] != "" {
			title = fmt.Sprintf(" title=\"%s\"", xmlEscape(t[0], true))
		}
		x.ln(2, fmt.Sprintf("<link%s>%s</link>", title, xmlEscape(t[1], false)))
	}
	x.ln(1, "</tutorials>")

	var methods []methodDoc
	for _, f := range c.funcs {
		if f.override || f.hidden != "" {
			continue
		}
		var qualifiers []string
		for _, q := range []struct {
			on   bool
			name string
		}{{f.virtual, "virtual"}, {f.isConst, "const"}, {f.static, "static"}} {
			if q.on {
				qualifiers = append(qualifiers, q.name)
			}
		}
		methods = append(methods, methodDoc{name: f.f.Name, qualifiers: strings.Join(qualifiers, " "), ret: f.ret,
			params: f.params, list: f.f.Params, doc: parseDoc(docText(f.f.Doc), false)})
	}
	x.methods(c, "method", methods)

	if len(c.vars) > 0 {
		vars := slices.Clone(c.vars)
		slices.SortStableFunc(vars, func(a, b *varModel) int {
			if naturalLess(a.v.Name, b.v.Name) {
				return -1
			}
			if naturalLess(b.v.Name, a.v.Name) {
				return 1
			}
			return 0
		})
		x.ln(1, "<members>")
		for _, v := range vars {
			vd := parseDoc(docText(v.v.Doc), false)
			x.ln(2, fmt.Sprintf("<member name=\"%s\"%s setter=\"%s\" getter=\"%s\"%s>", xmlEscape(v.v.Name, true),
				typeAttrs(c, v.t), xmlEscape(v.setter, true), xmlEscape(v.getter, true), docAttrs(vd)))
			x.ln(3, xmlEscape(strings.TrimFunc(vd.description, isDocSpace), false))
			x.ln(2, "</member>")
		}
		x.ln(1, "</members>")
	}

	var signals []methodDoc
	for _, s := range c.signals {
		signals = append(signals, methodDoc{name: s.s.Name, params: s.params, list: s.s.Params, doc: parseDoc(docText(s.s.Doc), false)})
	}
	x.methods(c, "signal", signals)

	if len(c.consts)+len(c.enums) > 0 {
		x.ln(1, "<constants>")
		constant := func(name string, value int64, enum *symbol, doc string) {
			cd := parseDoc(doc, false)
			attrs := ""
			if enum != nil {
				attrs = fmt.Sprintf(" enum=\"%s\"", xmlEscape(enum.name, true))
				if enum.bitfield {
					attrs += " is_bitfield=\"true\""
				}
			}
			x.ln(2, fmt.Sprintf("<constant name=\"%s\" value=\"%d\"%s%s>", xmlEscape(name, true), value, attrs, docAttrs(cd)))
			x.ln(3, xmlEscape(strings.TrimFunc(cd.description, isDocSpace), false))
			x.ln(2, "</constant>")
		}
		for _, k := range c.consts {
			constant(k.Name, k.Value.Value, nil, docText(k.Doc))
		}
		for _, e := range c.enums {
			for _, v := range e.values {
				constant(upperSnake(e.name)+"_"+v.Name, v.Value, e, v.Doc)
			}
		}
		x.ln(1, "</constants>")
	}
	x.ln(0, "</class>")
	return x.sb.String()
}

// documentAsyncClass returns the Godot XML documentation of the class of tasks named name.
func documentAsyncClass(name string) string {
	x := &xmlWriter{}
	x.ln(0, `<?xml version="1.0" encoding="UTF-8" ?>`)
	x.ln(0, fmt.Sprintf(`<class name="%s" inherits="RefCounted" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="https://raw.githubusercontent.com/godotengine/godot/master/doc/class.xsd">`,
		xmlEscape(name, true)))
	x.ln(1, "<brief_description>")
	x.ln(2, "A task: a job that runs on the [WorkerThreadPool], e.g. a call of an [code]@onthread[/code] function, and its result.")
	x.ln(1, "</brief_description>")
	x.ln(1, "<description>")
	for _, p := range []string{
		"A call of an [code]@onthread[/code] function of a GD++ class returns an object of this class: a task, whose job, the function's body, runs on the [WorkerThreadPool], while the caller goes on. Check [method is_done], or the [member done] property, once in a while, e.g. in [method Node._process], and take the result with [method claim] once it's done, like [code]claim[/code] in GD++ code. [method get_result], or the [member result] property, reads the result without taking it, [method wait] blocks until the job is done instead, and [method cancel] asks the job to stop early.",
		"When the last reference to a task is gone, it waits for its job to finish, if it still runs. So keep the task until [method is_done] is [code]true[/code].",
		"All its methods are thread-safe. Each package with GD++ classes has its own class of tasks, named after its prefix, e.g. [code]FooAsync[/code]. In GD++ code, the type [code]Async[lb]T[rb][/code] names it.",
	} {
		x.ln(2, p)
	}
	x.ln(1, "</description>")
	x.ln(1, "<tutorials>")
	x.ln(1, "</tutorials>")
	x.ln(1, "<methods>")
	for _, m := range []struct{ name, qualifiers, ret, doc string }{
		{"cancel", "", "void", "Asks the job to stop: in it, [code]is_cancelled[/code] is [code]true[/code] from now on, so it can return early. The job decides what it returns then. It does nothing if the job is done."},
		{"claim", "", "Variant", "Returns the job's result, and lets go of it: [method is_done] is [code]false[/code] from now on, and the task holds no result any more. Each result can be claimed once. The job must be done: see [method is_done]. In debug builds, claiming earlier, or twice, prints an error and returns [code]null[/code]; in release builds, it's undefined."},
		{"get_result", "const", "Variant", "Returns the job's result, and keeps it, unlike [method claim]. The job must be done, and its result not claimed: see [method is_done]. In debug builds, calling it earlier, or after [method claim], prints an error and returns [code]null[/code]; in release builds, it's undefined."},
		{"is_done", "const", "bool", "Returns [code]true[/code] once the job has finished, so [method claim] and [method get_result] return its result, and [method wait] returns right away. It's [code]false[/code] again once the result is claimed. An object that runs no job, e.g. from [code]new()[/code], is done, and its result is [code]null[/code]."},
		{"wait", "", "Variant", "Waits for the job to finish, and returns its result, and keeps it, like [method get_result]. It blocks the calling thread until then: on the main thread, the game stops, so prefer checking [method is_done]. After [method claim], there's no result any more: in debug builds, it prints an error and returns [code]null[/code]."},
	} {
		attrs := ""
		if m.qualifiers != "" {
			attrs = fmt.Sprintf(" qualifiers=\"%s\"", m.qualifiers)
		}
		x.ln(2, fmt.Sprintf("<method name=\"%s\"%s>", m.name, attrs))
		x.ln(3, fmt.Sprintf("<return type=\"%s\" />", m.ret))
		x.ln(3, "<description>")
		x.ln(4, m.doc)
		x.ln(3, "</description>")
		x.ln(2, "</method>")
	}
	x.ln(1, "</methods>")
	x.ln(1, "<members>")
	x.ln(2, `<member name="done" type="bool" setter="" getter="is_done">`)
	x.ln(3, "Whether the job has finished, and its result hasn't been claimed, read-only: the same as [method is_done].")
	x.ln(2, "</member>")
	x.ln(2, `<member name="result" type="Variant" setter="" getter="get_result">`)
	x.ln(3, "The job's result, read-only: the same as [method get_result], with its checks. The job must be done, and its result not claimed: see [method is_done].")
	x.ln(2, "</member>")
	x.ln(1, "</members>")
	x.ln(0, "</class>")
	return x.sb.String()
}
