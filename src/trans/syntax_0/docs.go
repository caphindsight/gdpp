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
	if name := c.newName(); name != "" {
		methods = append(methods, methodDoc{name: name, qualifiers: "static", ret: &gtype{cpp: c.name + " *", doc: c.name},
			doc: parseDoc(newDocs[name], false)})
	}
	if c.pool != nil {
		void := &gtype{cpp: "void", doc: "void", void: true}
		methods = append(methods, methodDoc{name: "free_pooled", ret: void, doc: parseDoc(freePooledDoc, false)},
			methodDoc{name: "queue_free_pooled", ret: void, doc: parseDoc(queueFreePooledDoc, false)},
			methodDoc{name: "pool_reserve", qualifiers: "static", ret: void, params: []*gtype{{cpp: "int64_t", doc: "int"}, {cpp: "String", doc: "String", byRef: true}},
				list: []*Param{{Name: "count"}, {Name: "mode", Default: &Default{Expr: `""`}}}, doc: parseDoc(poolReserveDoc, false)},
			methodDoc{name: "pool_clear", qualifiers: "static", ret: void, params: []*gtype{{cpp: "bool", doc: "bool"}},
				list: []*Param{{Name: "keep_in_use", Default: &Default{Expr: "false"}}}, doc: parseDoc(poolClearDoc, false)})
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

// The documentation of the methods that @pool and @scene classes have for scripts, by name.
var newDocs = map[string]string{
	"new_scene":        `Creates an instance of the class's scene, like [code]create[/code] in GD++ code, and returns its root. The scene is loaded once, by the first call. Returns [code]null[/code] if the scene fails to load, or if its root isn't an object of this class, so always check the result. [code]new()[/code] creates the node alone, without the scene.`,
	"new_pooled":       `Takes an object from the class's pool, or creates a new one if the pool has none, like [code]create[/code] in GD++ code. Returns [code]null[/code] once all the objects of a [code]"fixed"[/code] pool are in use, so always check the result. Return the object with [method free_pooled] or [method queue_free_pooled], not [method Node.queue_free], or it won't be reused. [code]new()[/code] creates an object outside the pool.`,
	"new_scene_pooled": `Takes an object from the class's pool, or creates a new one, an instance of the class's scene, if the pool has none, like [code]create[/code] in GD++ code. Returns [code]null[/code] once all the objects of a [code]"fixed"[/code] pool are in use, or if the scene fails to load, or its root isn't an object of this class, so always check the result. Return the object with [method free_pooled] or [method queue_free_pooled], not [method Node.queue_free], or it won't be reused. [code]new()[/code] creates the node alone, outside the pool and without the scene.`,
}

const freePooledDoc = `Returns the node to its pool right away, like [code]destroy[/code] in GD++ code: the node is removed from the tree, and kept for reuse. Calling it again before it's reused does nothing. Godot doesn't allow removing a collision object from the tree during a physics callback, e.g. in a handler of [signal Area3D.body_entered], so use [method queue_free_pooled] there. A node that the pool didn't make is freed at the end of the frame, like with [method Node.queue_free].
[b]Warning:[/b] a returned node stays a valid object, and the pool may hand it out again at any time, so drop every reference to it. [method @GlobalScope.is_instance_valid] can't tell that it was returned.`

const poolReserveDoc = `Makes or frees resting objects, so that the class's pool has [param count] objects, counting both those in use and those resting, e.g. when a level loads. A new object runs its constructor, but waits unused until the pool hands it out. Objects in use are never freed. With [param mode], [code]"fixed"[/code] or [code]"grow"[/code], [param count] becomes the pool's size first, and [param mode] its mode: once all the objects of the size are in use, a [code]"fixed"[/code] pool makes no more, while a [code]"grow"[/code] pool makes more. Without it, the size and mode stay as they are, and a [code]"fixed"[/code] pool makes at most its size, with an error if [param count] is more. [code]pool_reserve(0)[/code] frees all the resting objects.
[b]Warning:[/b] it isn't thread-safe, so call it from the main thread only.`

const poolClearDoc = `Drops all the objects of the class's pool for good: it frees the resting ones right away, and those in use at the end of the frame, like with [method Node.queue_free]. With [param keep_in_use], it leaves those in use alone instead: they no longer belong to the pool, so [method free_pooled] frees them. The pool's size and mode stay as they are.
[b]Warning:[/b] the objects in use are freed, so drop every reference to them, unless [param keep_in_use] is [code]true[/code]. It isn't thread-safe, so call it from the main thread only.`

const queueFreePooledDoc = `Returns the node to its pool at the end of the frame, like [code]queue_destroy[/code] in GD++ code: the node is removed from the tree, and kept for reuse. Calling it again before then does nothing. A node that the pool didn't make is freed, like with [method Node.queue_free].
[b]Warning:[/b] a returned node stays a valid object, and the pool may hand it out again at any time, so drop every reference to it. [method @GlobalScope.is_instance_valid] can't tell that it was returned.`

// documentAsyncClass returns the Godot XML documentation of the class of tasks named name.
func documentAsyncClass(name string) string {
	x := &xmlWriter{}
	// text writes each line of s, keeping empty lines, which code blocks may have.
	text := func(tabs int, s string) {
		for _, line := range strings.Split(s, "\n") {
			if line == "" {
				x.sb.WriteString("\n")
			}
			x.ln(tabs, line)
		}
	}
	x.ln(0, `<?xml version="1.0" encoding="UTF-8" ?>`)
	x.ln(0, fmt.Sprintf(`<class name="%s" inherits="RefCounted" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="https://raw.githubusercontent.com/godotengine/godot/master/doc/class.xsd">`,
		xmlEscape(name, true)))
	x.ln(1, "<brief_description>")
	x.ln(2, "A task: a job that runs on the [WorkerThreadPool], e.g. a call of an [code]@onthread[/code] function, and its result.")
	x.ln(1, "</brief_description>")
	x.ln(1, "<description>")
	text(2, `GD++ provides this class: its runtime defines it, and the package registers it. Run [code]gd++ man async[/code] to read how to use it, and [code]gd++ trans --runtime[/code] to see how it's implemented.
A call of an [code]@onthread[/code] function of a GD++ class returns an object of this class: a task. Its job, the function's body, runs on the [WorkerThreadPool], while the caller goes on. Once the job is done, the caller takes its result.
A task is in one of three states. It goes through them in this order, and never goes back:
[b]1. Running:[/b] the job runs.
- [member valid] is [code]true[/code], and [member done] is [code]false[/code].
- [method wait] blocks until the job is done, then returns its result.
- [method claim] and [member result] are errors: there's no result yet.
- [method cancel] asks the job to stop.
[b]2. Done:[/b] the job has finished, and the task holds its result.
- [member valid] and [member done] are [code]true[/code].
- [member result] and [method wait] return the result, as often as you like.
- [method claim] returns the result, and moves the task to Claimed.
- [method cancel] does nothing.
[b]3. Claimed:[/b] [method claim] took the result, and the task holds nothing any more.
- [member valid] and [member done] are [code]false[/code].
- [method claim], [member result] and [method wait] are errors: the result is gone.
- [method cancel] does nothing.
In debug builds, an error prints a message, and returns [code]null[/code]. Release builds don't check, so an error is undefined behavior there. A task made with [code]new()[/code] runs no job: it starts Claimed, so [member valid] and [member done] are [code]false[/code], like an empty [code]Async[/code] in GD++ code.
Check [member done] once a frame, e.g. in [method Node._process], and claim the result once it's [code]true[/code]. Here, a character follows a path that a worker thread finds, and starts the next search when it has one:
[codeblock]
var search = null

func _process(_delta):
	if search == null:
		search = $Pathfinder.find_path(position, target)
	elif search.done:
		follow(search.claim())
		search = null
[/codeblock]
[member valid] tells a Running task from a Claimed one, since neither is [member done]. Test [member valid], not the task object itself: [code]if loading:[/code] is [code]true[/code] for a Claimed task too. Here, a level loads in the background, a spinner shows while it does, and a button cancels it, after which the job may return [code]null[/code]:
[codeblock]
@onready var loading = $Levels.load_level("forest")

func _process(_delta):
	$Spinner.visible = loading.valid and not loading.done
	if loading.done:
		var level = loading.claim()
		if level:
			add_child(level)

func _on_cancel_pressed():
	loading.cancel()
[/codeblock]
When the last reference to a task is gone, it waits for its job to finish, if it still runs: so keep the task until [member done] is [code]true[/code]. All its members are thread-safe. Each package whose GD++ classes use Async has its own class of tasks, named after its prefix, e.g. [code]FooAsync[/code]. In GD++ code, the type [code]Async[T][/code] names it.`)
	x.ln(1, "</description>")
	x.ln(1, "<tutorials>")
	x.ln(1, "</tutorials>")
	x.ln(1, "<methods>")
	for _, m := range []struct{ name, ret, doc string }{
		{"cancel", "void", "Asks the job to stop, while the task is Running: in the job, [code]is_cancelled[/code] is [code]true[/code] from now on, so it can return early. What it returns then is its result, as usual, and the task gets Done. Does nothing when the task is Done or Claimed."},
		{"claim", "Variant", "Returns the result, and lets go of it: the task moves from Done to Claimed, so each result is claimed once. Call it when [member done] is [code]true[/code]. While the task is Running, or once it's Claimed, it's an error: in debug builds, it prints a message and returns [code]null[/code]; in release builds, it's undefined behavior."},
		{"wait", "Variant", "Returns the result, and keeps it, like [member result], but while the task is Running, it first blocks until the job is done. On the main thread, the game freezes while it waits, so prefer checking [member done] once a frame. Once the task is Claimed, it's an error: in debug builds, it prints a message and returns [code]null[/code]; in release builds, it's undefined behavior."},
	} {
		x.ln(2, fmt.Sprintf("<method name=\"%s\">", m.name))
		x.ln(3, fmt.Sprintf("<return type=\"%s\" />", m.ret))
		x.ln(3, "<description>")
		x.ln(4, m.doc)
		x.ln(3, "</description>")
		x.ln(2, "</method>")
	}
	x.ln(1, "</methods>")
	x.ln(1, "<members>")
	for _, m := range []struct{ name, typ, getter, doc string }{
		{"done", "bool", "is_done", "[code]true[/code] when the task is Done: the job has finished, and its result can be claimed. [code]false[/code] while the task is Running, and once it's Claimed. Read-only."},
		{"result", "Variant", "get_result", "The job's result, when the task is Done. Reading it keeps the result, unlike [method claim], so it can be read as often as you like. In GD++ code, [code]task.result()[/code] does the same. While the task is Running, or once it's Claimed, it's an error: in debug builds, it prints a message and returns [code]null[/code]; in release builds, it's undefined behavior. Read-only. The inspector doesn't show it, since it would read it before the job is done."},
		{"valid", "bool", "is_valid", "[code]true[/code] while the task is Running or Done, and [code]false[/code] once it's Claimed: whether the task has a result, or will have one. In GD++ code, testing an [code]Async[/code], e.g. [code]if (task)[/code], gives the same. In scripts, test [member valid], not the task object: [code]if task:[/code] only checks that it isn't [code]null[/code], so it's [code]true[/code] for a Claimed task too. With [member done], it tells the three states apart: the task is Running when it's [member valid] but not [member done]. Read-only."},
	} {
		x.ln(2, fmt.Sprintf(`<member name="%s" type="%s" setter="" getter="%s">`, m.name, m.typ, m.getter))
		x.ln(3, m.doc)
		x.ln(2, "</member>")
	}
	x.ln(1, "</members>")
	x.ln(0, "</class>")
	return x.sb.String()
}
