package syntax_1

import (
	"fmt"

	"gd++/trans/meta"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/alecthomas/participle/v2/lexer"
)

// gpuModel is a shader: a function whose body is GLSL, which runs on the GPU, once for each cell of its grid, its
// first parameter. Without a GPU, it runs on the CPU: its body also compiles as C++, see glslToCpp.
type gpuModel struct {
	s      *Shader
	dims   int         // The grid's dimensions: 1, 2 or 3, for an int, a Vector2i or a Vector3i.
	group  [3]int      // The size of its workgroups, from @grid or the default for dims.
	params []*gpuParam // Its parameters, in order.
	grid   int         // The index of its grid in params: its first parameter, or the one that @grid names.
	ret    gpuResult
	main   bool // Whether it runs on the main RenderingDevice, since it uses GPU arrays or textures.
	async  bool // With @async: a call returns right away, while the GPU works, instead of once it's done with the shader.
}

// gpuParam is a parameter of a shader.
type gpuParam struct {
	p      *Param
	kind   string // "grid" for the grid, "uniform" for scalars, "buffer" for packed arrays, "array" for GPU arrays, "sampler" for an Image or a Texture2D, "image" for a Texture2D[FORMAT].
	glsl   string // Its GLSL type, e.g. "vec3", or the element type of an array.
	format string // For an image: its format, e.g. "rgba8".
	main   bool   // Whether it's on the main RenderingDevice.
}

// gpuResult is what a shader returns: what its body returns for each cell, put together.
type gpuResult struct {
	kind   string // "void", "buffer" for a packed array, "array" for a GPU array, "image" for an Image, "texture" for a Texture2D[FORMAT].
	glsl   string // The GLSL type that the body returns.
	cpp    string // For a buffer, the packed array; for an array, its element's C++ type.
	format string // For an image or a texture: its format.
}

// gpuUniforms are the types of the scalars that shaders take, by GD++ type, with their GLSL type.
var gpuUniforms = map[string]string{"int": "int", "float": "float", "bool": "bool", "Vector2": "vec2", "Vector2i": "ivec2",
	"Vector3": "vec3", "Vector3i": "ivec3", "Vector4": "vec4", "Vector4i": "ivec4", "Color": "vec4", "Basis": "mat3",
	"Transform3D": "mat4x3", "Projection": "mat4"}

// gpuPacked are the packed arrays that shaders read and return, with the GLSL type of their elements.
var gpuPacked = map[string]string{"PackedFloat32Array": "float", "PackedInt32Array": "int", "PackedVector2Array": "vec2",
	"PackedVector3Array": "vec3", "PackedVector4Array": "vec4", "PackedColorArray": "vec4"}

// gpuFormats are the formats of the images and textures that shaders write, named like Godot's Image formats, in
// lower case, with how many channels they have.
var gpuFormats = map[string]int{"r8": 1, "rg8": 2, "rgb8": 3, "rgba8": 4, "rf": 1, "rgf": 2, "rgbh": 3, "rgbah": 4, "rgbf": 3, "rgbaf": 4}

// glslFormats are the GLSL names of gpuFormats, which image layouts take. GPUs can't write 3 channels, so a shader
// writes an RGB format as RGBA, and the runtime converts the Image it returns.
var glslFormats = map[string]string{"r8": "r8", "rg8": "rg8", "rgb8": "rgba8", "rgba8": "rgba8", "rf": "r32f", "rgf": "rg32f",
	"rgbh": "rgba16f", "rgbah": "rgba16f", "rgbf": "rgba32f", "rgbaf": "rgba32f"}

// gpuFormatList and gpuTextureFormatList are the names of the formats of images and of textures, for messages.
const (
	gpuFormatList        = "r8, rg8, rgb8, rgba8, rf, rgf, rgbh, rgbah, rgbf or rgbaf"
	gpuTextureFormatList = "r8, rg8, rgba8, rf, rgf, rgbah or rgbaf"
)

// rgbFormatError returns the error for the RGB format f of a texture, at its type argument t, a parameter's if param.
func (u *unit) rgbFormatError(t *Type, f string, param bool) error {
	rgba := f[:len(f)-1] + "a" + f[len(f)-1:]
	hint := fmt.Sprintf("Use %s, and return vec4(color, 1.0). Only an Image[%s] can have an RGB format.", rgba, f)
	if param {
		hint = fmt.Sprintf("Use %s: imageStore takes a vec4 anyway, e.g. vec4(color, 1.0).", rgba)
	}
	return u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Textures can't have the format %s, since GPUs can't write 3 channels.", f), hint)
}

// gpuGrids are the types of a shader's grid, with their GLSL types, by dimensions.
var gpuGrids = map[string]int{"int": 1, "Vector2i": 2, "Vector3i": 3}

// gpuIds are the GLSL types of a cell, by dimensions.
var gpuIds = [4]string{"", "int", "ivec2", "ivec3"}

// gpuDefaultGroups are the default workgroup sizes, by dimensions.
var gpuDefaultGroups = [4][3]int{{}, {64, 1, 1}, {8, 8, 1}, {4, 4, 4}}

// glslReserved are the names that a shader's parameters can't have: GLSL's keywords, and the names that GD++ adds.
var glslReserved = []string{"id", "index", "attribute", "const", "uniform", "varying", "buffer", "shared", "coherent", "volatile",
	"restrict", "readonly", "writeonly", "layout", "centroid", "flat", "smooth", "noperspective", "patch", "sample", "break",
	"continue", "do", "for", "while", "switch", "case", "default", "if", "else", "subroutine", "in", "out", "inout", "true",
	"false", "invariant", "precise", "discard", "return", "struct", "void", "lowp", "mediump", "highp", "precision", "input",
	"output", "filter", "texture", "main", "not"}

// formatChannels returns the GLSL type of a pixel of a format with n channels: float, vec2, vec3 or vec4.
func formatChannels(n int) string {
	return map[int]string{1: "float", 2: "vec2", 3: "vec3", 4: "vec4"}[n]
}

// buildShader checks s, a shader of the class named owner, and returns it as the static function that runs it.
func (u *unit) buildShader(s *Shader, owner string) (*funcModel, error) {
	a, err := u.annotations(s.Annotations, "a shader", "async", "editor_only", "game_only", "grid", "noprofile", "notrace", "onthread", "profile", "static", "trace")
	if err != nil {
		return nil, err
	}
	if st := a["static"]; st != nil {
		return nil, u.errorAt(st.Pos, len(st.Name)+1, "A shader is always static: remove @static.", "Shaders can't use an object's fields, so GD++ makes them static.")
	}
	g := &gpuModel{s: s}
	m := &funcModel{f: &Func{Pos: s.Pos, Doc: s.Doc, Annotations: s.Annotations, Name: s.Name, Params: s.Params, Return: s.Return},
		static: true, gpu: g}
	if m.only, err = u.onlyOf(a, owner); err != nil {
		return nil, err
	}
	if a["onthread"] != nil {
		m.deferral = "onthread"
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
	if m.trace, err = u.debugOn(a["trace"], owner); err != nil {
		return nil, err
	}
	if m.profile, err = u.debugOn(a["profile"], owner); err != nil {
		return nil, err
	}
	m.notrace, m.noprofile = a["notrace"] != nil, a["noprofile"] != nil
	if len(s.Params) == 0 {
		return nil, u.errorAt(s.Pos, len("shader"), fmt.Sprintf("Shader %s needs a parameter for the size of its grid: an int, Vector2i or Vector3i.", s.Name),
			"The shader runs once for each cell of its grid, e.g. \"shader blur(size: Vector2i, ...)\". It's the first parameter, unless @grid names another one.")
	}
	// @grid's arguments: the grid's parameter, as a name or a string, then the workgroup's sizes, each optional.
	sizes := argsOf(a["grid"])
	if len(sizes) > 0 && !numberRegexp.MatchString(sizes[0].Value) {
		arg := sizes[0]
		name, err := strconv.Unquote(arg.Value)
		if err != nil {
			name = arg.Value
		}
		if g.grid = slices.IndexFunc(s.Params, func(p *Param) bool { return p.Name == name }); g.grid < 0 {
			return nil, u.errorAt(arg.Pos, len(arg.Value), fmt.Sprintf("Shader %s has no parameter %s.", s.Name, name),
				"@grid's first argument may name the parameter that is the shader's grid, e.g. @grid(size) or @grid(size, 8, 8).")
		}
		sizes = sizes[1:]
	}
	for i, p := range s.Params {
		gp, t, err := u.shaderParam(p, i == g.grid)
		if err != nil {
			return nil, err
		}
		if i == g.grid {
			g.dims = gpuGrids[p.Type.Name]
		}
		g.params = append(g.params, gp)
		g.main = g.main || gp.main
		m.params = append(m.params, t)
		if p.Default == nil && i > 0 && s.Params[i-1].Default != nil {
			return nil, u.errorAt(p.Pos, len(p.Name), fmt.Sprintf("Parameter %s needs a default value, since a parameter before it has one.", p.Name),
				"Parameters with default values must come last.")
		}
	}
	if g.ret, m.ret, err = u.shaderResult(s, g.dims); err != nil {
		return nil, err
	}
	g.main = g.main || g.ret.kind == "array" || g.ret.kind == "texture"
	if m.detached && !m.ret.void {
		return nil, u.errorAt(s.Pos, len("shader"), fmt.Sprintf("The @onthread(\"detached\") shader %s must return void.", s.Name),
			"Nothing waits for its task, so its calls can't return a value.")
	}
	g.async = a["async"] != nil
	if args := argsOf(a["async"]); len(args) > 0 {
		return nil, u.errorAt(args[0].Pos, len(args[0].Value), "Annotation @async takes no arguments on a shader.", "")
	}
	if g.async && (g.ret.kind == "buffer" || g.ret.kind == "image") {
		what := map[string]string{"buffer": "a " + s.Return.Name, "image": "an Image"}[g.ret.kind]
		hint := "Remove @async. Or use @onthread instead, so the caller gets an Async and doesn't wait."
		if m.deferral == "onthread" {
			hint = "Remove @async: its task waits for the GPU, while the caller gets an Async right away."
		}
		if g.ret.kind == "image" {
			hint += fmt.Sprintf(" Or return a Texture2D[%s] instead: it stays on the GPU, so it can be @async.", g.ret.format)
		}
		as := a["async"]
		return nil, u.errorAt(as.Pos, len(as.Name)+1, fmt.Sprintf("Shader %s returns %s, which comes back from the GPU, so it can't be @async.", s.Name, what), hint)
	}
	g.group = gpuDefaultGroups[g.dims]
	if wg := a["grid"]; len(sizes) > 0 {
		if len(sizes) != g.dims {
			return nil, u.errorAt(wg.Pos, len(wg.Name)+1, fmt.Sprintf("Annotation @grid takes %d workgroup sizes here, one for each dimension of the grid of shader %s.", g.dims, s.Name),
				fmt.Sprintf("E.g. @grid(%s).", strings.Trim(strings.Join(strings.Fields(fmt.Sprint(gpuDefaultGroups[g.dims][:g.dims])), ", "), "[]")))
		}
		total := 1
		for i, arg := range sizes {
			n, err := strconv.Atoi(arg.Value)
			if err != nil || n < 1 {
				return nil, u.errorAt(arg.Pos, len(arg.Value), "Annotation @grid takes positive integers as workgroup sizes, after the grid's parameter if any.", "E.g. @grid(64), @grid(8, 8) or @grid(size, 8, 8).")
			}
			g.group[i], total = n, total*n
		}
		if total > 1024 {
			return nil, u.errorAt(wg.Pos, len(wg.Name)+1, fmt.Sprintf("The workgroups of shader %s have %d invocations, but GPUs run at most 1024.", s.Name, total),
				"Choose smaller sizes: 64 or 256 invocations work well everywhere.")
		}
	}
	return m, nil
}

// shaderNotVirtual returns an error if shader f has the name of a virtual function of base, or of a trait function that
// base implements: shaders are static, so they can't override them.
func (u *unit) shaderNotVirtual(f *funcModel, base string) error {
	owner, _ := u.virtualOwner(base, f.f.Name)
	if trait := u.traitFuncOwner(base, f.f.Name); owner == "" && trait != "" {
		owner = "trait " + trait
	}
	if owner == "" {
		return nil
	}
	return u.errorAt(f.f.Pos, len("shader"), fmt.Sprintf("Shader %s has the name of a virtual function of %s, which it can't override, since shaders are static.", f.f.Name, owner),
		"Rename the shader. To override the function, write a func that calls the shader.")
}

// numberRegexp matches a number, as an annotation's argument.
var numberRegexp = regexp.MustCompile(`^-?[0-9]`)

// shaderParam checks p, a parameter of a shader, or with grid, its grid. It returns the parameter, and the type
// of the generated function's parameter.
func (u *unit) shaderParam(p *Param, grid bool) (*gpuParam, *gtype, error) {
	if slices.Contains(glslReserved, p.Name) {
		return nil, nil, u.errorAt(p.Pos, len(p.Name), fmt.Sprintf("A shader's parameter can't be named %s, a name of GLSL or of GD++ in shaders.", p.Name), "Rename the parameter.")
	}
	t := p.Type
	if t == nil {
		return nil, nil, u.errorAt(p.Pos, len(p.Name), fmt.Sprintf("Parameter %s of a shader needs a type.", p.Name), "Shaders don't take Variants, e.g. \"scale: float\".")
	}
	gp := &gpuParam{p: p}
	if grid {
		if gpuGrids[t.Name] == 0 || len(t.Args) > 0 {
			hint := "The shader runs once for each cell of its grid, e.g. \"shader blur(size: Vector2i, ...)\". It's the first parameter, unless @grid names another one."
			if i := strings.Index("Vector2Vector3", t.Name); i >= 0 && len(t.Args) == 0 {
				hint = fmt.Sprintf("A grid counts cells, so its size is whole: write %si.", t.Name)
			}
			return nil, nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("The grid of a shader is an int, Vector2i or Vector3i, but %s is a %s.", p.Name, typeString(t)), hint)
		}
		gp.kind, gp.glsl = "grid", gpuIds[gpuGrids[t.Name]]
		gt, err := u.resolve(t, false)
		return gp, gt, err
	}
	switch {
	case t.Name == "GpuArray":
		gt, err := u.resolve(t, false)
		if err != nil {
			return nil, nil, err
		}
		gp.kind, gp.glsl, gp.main = "array", gt.gpu, true
		return gp, gt, nil
	case t.Name == "Texture2D" && len(t.Args) == 1:
		f := t.Args[0]
		if gpuFormats[f.Name] == 0 || len(f.Args) > 0 {
			return nil, nil, u.errorAt(f.Pos, len(f.Name), fmt.Sprintf("Unknown texture format %s.", f.Name), "Shaders write textures of format "+gpuTextureFormatList+".")
		}
		if gpuFormats[f.Name] == 3 {
			return nil, nil, u.rgbFormatError(f, f.Name, true)
		}
		gt, err := u.resolve(&Type{Pos: t.Pos, Name: t.Name}, false)
		gp.kind, gp.glsl, gp.format, gp.main = "image", "image2D", f.Name, true
		return gp, gt, err
	case len(t.Args) > 0:
	case t.Name == "Image" || t.Name == "Texture2D":
		gt, err := u.resolve(t, false)
		gp.kind, gp.glsl, gp.main = "sampler", "sampler2D", t.Name == "Texture2D"
		return gp, gt, err
	case gpuUniforms[t.Name] != "":
		gt, err := u.resolve(t, false)
		gp.kind, gp.glsl = "uniform", gpuUniforms[t.Name]
		return gp, gt, err
	case gpuPacked[t.Name] != "":
		gt, err := u.resolve(t, false)
		gp.kind, gp.glsl = "buffer", gpuPacked[t.Name]
		return gp, gt, err
	}
	return nil, nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Shaders can't take a %s.", typeString(t)),
		"Shaders take scalars and vectors, e.g. float, Vector3 and Color, packed arrays of floats, ints and vectors, GpuArray[T], Image, Texture2D, and Texture2D[FORMAT] to write.")
}

// shaderResult checks the return type of s, whose grid has dims dimensions. It returns the result, and the type that
// the generated function returns.
func (u *unit) shaderResult(s *Shader, dims int) (gpuResult, *gtype, error) {
	t := s.Return
	if t == nil {
		return gpuResult{}, nil, u.errorAt(s.Pos, len("shader"), fmt.Sprintf("Shader %s needs a return type: void, or what it puts together from each cell's result.", s.Name),
			"E.g. -> PackedFloat32Array, -> Image[rf], -> Texture2D[rgba8], -> GpuArray[float], or -> void.")
	}
	needs2d := func(what string) error {
		if dims == 2 {
			return nil
		}
		return u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Shader %s returns %s, so its grid must be a Vector2i.", s.Name, what), "Each cell of the grid makes one pixel.")
	}
	format := func(name string, a []*Type) (string, error) {
		if len(a) == 0 {
			return "", nil
		}
		if len(a) > 1 || gpuFormats[a[0].Name] == 0 || len(a[0].Args) > 0 {
			return "", u.errorAt(a[0].Pos, len(a[0].Name), fmt.Sprintf("Unknown %s format %s.", strings.ToLower(name), a[0].Name), "Shaders write "+gpuFormatList+".")
		}
		return a[0].Name, nil
	}
	switch {
	case t.Name == "void" && len(t.Args) == 0:
		gt, err := u.resolve(t, true)
		return gpuResult{kind: "void"}, gt, err
	case gpuPacked[t.Name] != "" && len(t.Args) == 0:
		gt, err := u.resolve(t, false)
		return gpuResult{kind: "buffer", glsl: gpuPacked[t.Name], cpp: t.Name}, gt, err
	case t.Name == "GpuArray":
		gt, err := u.resolve(t, false)
		if err != nil {
			return gpuResult{}, nil, err
		}
		return gpuResult{kind: "array", glsl: gt.gpu, cpp: gpuElements[t.Args[0].Name][0]}, gt, nil
	case t.Name == "Image" || t.Name == "Texture2D":
		f, err := format(t.Name, t.Args)
		switch {
		case err != nil:
			return gpuResult{}, nil, err
		case f == "" && t.Name == "Texture2D":
			return gpuResult{}, nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Shader %s returns a texture, so it needs its format, e.g. -> Texture2D[rgba8].", s.Name),
				"Formats: "+gpuTextureFormatList+".")
		case t.Name == "Texture2D" && gpuFormats[f] == 3:
			return gpuResult{}, nil, u.rgbFormatError(t.Args[0], f, false)
		case needs2d("an image") != nil:
			return gpuResult{}, nil, needs2d("an image")
		}
		f = cmpOr(f, "rgbaf")
		gt, err := u.resolve(&Type{Pos: t.Pos, Name: t.Name}, false)
		kind := map[string]string{"Image": "image", "Texture2D": "texture"}[t.Name]
		return gpuResult{kind: kind, glsl: formatChannels(gpuFormats[f]), format: f}, gt, err
	}
	return gpuResult{}, nil, u.errorAt(t.Pos, len(t.Name), fmt.Sprintf("Shaders can't return a %s.", typeString(t)),
		"Shaders return void, a packed array, a GpuArray[T], an Image[FORMAT] or a Texture2D[FORMAT], with what their body returns for each cell.")
}

// cmpOr returns a, or b if a is empty.
func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// shaderNamespace is the namespace of class c's shaders' code, at global scope.
func shaderNamespace(c *classModel) string {
	return "_gdpp_gpu_" + c.name
}

// shaderStruct is the C++ struct of shader f: its GLSL, and its body as C++, for the CPU.
func shaderStruct(f *funcModel) string {
	return "_gdpp_shader_" + f.gpu.s.Name
}

// shaderUses reports whether the GLSL code uses one of names.
func shaderUses(code string, names ...string) bool {
	return slices.ContainsFunc(identifiers(code), func(id string) bool { return slices.Contains(names, id) })
}

// sharedRegexp matches the declaration of a shared variable, and its name.
var sharedRegexp = regexp.MustCompile(`\bshared\s+[A-Za-z_][A-Za-z0-9_]*\s+([A-Za-z_][A-Za-z0-9_]*)`)

// structRegexp matches the declaration of a struct, and its name.
var structRegexp = regexp.MustCompile(`\bstruct\s+([A-Za-z_][A-Za-z0-9_]*)`)

// shaderLib is a shader block: of the package, outside of classes, or of a class.
type shaderLib struct {
	b      *Block
	file   string // How GLSL's #line names its file: its path.
	source string // How C++'s #line names its file, or empty for this file, whose name the writer knows.
}

// shaderLibs returns the package's shader blocks outside of classes: those of file, the expanded GD++ file filename,
// whose source is src, and those of the files that opts.Dependencies names, in the order of their files' paths.
func shaderLibs(filename, src string, file *File, opts meta.Options) ([]shaderLib, error) {
	var libs []shaderLib
	for _, code := range file.ShaderBlocks {
		libs = append(libs, shaderLib{b: code.Body, file: filename})
	}
	for _, d := range opts.Dependencies {
		if d.Kind != meta.ShaderLibrary || d.File == filename {
			continue
		}
		f, err := Parse(d.File, d.Source)
		if err != nil {
			return nil, err
		}
		if len(f.Invokes) > 0 { // They may generate shader blocks.
			if f, _, err = parseExpanded(d.File, d.Source, depOptions(filename, src, d, opts)); err != nil {
				return nil, err
			}
		}
		for _, code := range f.ShaderBlocks {
			libs = append(libs, shaderLib{b: code.Body, file: d.File, source: cmpOr(d.SourceName, d.File)})
		}
	}
	slices.SortStableFunc(libs, func(a, b shaderLib) int { return strings.Compare(a.file, b.file) })
	return libs, nil
}

// depOptions returns the options to expand d's file with: opts, with the macros, templates and macro libraries of
// filename, whose source is src, which opts.Dependencies leaves out.
func depOptions(filename, src string, d meta.Dependency, opts meta.Options) meta.Options {
	decls, _ := ListMacros(filename, src) // src already parsed.
	kinds := map[meta.DeclKind]meta.Kind{meta.MacroDecl: meta.Macro, meta.TemplateDecl: meta.Template, meta.LibraryDecl: meta.MacroLibrary}
	var deps []meta.Dependency
	for _, decl := range decls {
		if kind, ok := kinds[decl.Kind]; ok {
			deps = append(deps, meta.Dependency{Name: decl.Name, Kind: kind, Source: src, File: filename, SourceName: opts.SourceName})
		}
	}
	opts.Dependencies, opts.SourceName = append(deps, opts.Dependencies...), d.SourceName
	return opts
}

// shaderBlocks returns the shader blocks that class c's shaders use: the package's, then the class's own.
func (u *unit) shaderBlocks(c *classModel) []shaderLib {
	libs := slices.Clone(u.shaderLibs)
	for _, code := range c.shaders {
		libs = append(libs, shaderLib{b: code.Body, file: u.file.Pos.Filename})
	}
	return libs
}

// hasCPU reports whether shader f of class c runs on the CPU too: not if its body uses barrier() or the class's shared
// variables, since the CPU runs a workgroup's invocations one after another.
func (u *unit) hasCPU(c *classModel, f *funcModel) bool {
	names := []string{"barrier"}
	for _, lib := range u.shaderBlocks(c) {
		for _, m := range sharedRegexp.FindAllStringSubmatch(lib.b.Text, -1) {
			names = append(names, m[1])
		}
	}
	return !shaderUses(f.gpu.s.Body.Text, names...)
}

// shaderDefs writes the code of class c's shaders, at global scope: for each one, a struct with its GLSL, and its body
// as C++, for the CPU. Nothing if c has no shaders.
func (u *unit) shaderDefs(w *writer, c *classModel) {
	var shaders []*funcModel
	for _, f := range c.funcs {
		if f.gpu != nil && f.hidden == "" {
			shaders = append(shaders, f)
		}
	}
	if len(shaders) == 0 {
		return
	}
	var structs []string
	for _, lib := range u.shaderBlocks(c) {
		for _, m := range structRegexp.FindAllStringSubmatch(lib.b.Text, -1) {
			structs = append(structs, m[1])
		}
	}
	w.ln("")
	w.ln("namespace %s {", shaderNamespace(c))
	w.ln("")
	w.ln("using namespace gdpp::glsl;")
	for _, lib := range u.shaderBlocks(c) {
		w.ln("")
		w.gpu(lib.b, lib.source, "", "", structs)
	}
	for _, f := range shaders {
		g := f.gpu
		w.ln("")
		w.ln("struct %s {", shaderStruct(f))
		w.ln("\tusing Id = %s;", gpuIds[g.dims])
		w.ln("\tstatic constexpr const char *glsl =")
		lines := strings.Split(strings.TrimRight(u.glsl(c, f), "\n"), "\n")
		for i, line := range lines {
			end := ""
			if i == len(lines)-1 {
				end = ";"
			}
			w.ln("\t\t\t%s%s", glslString(line+"\n"), end)
		}
		for _, p := range g.params {
			w.ln("\t%s %s;", cpuType(p), p.p.Name)
		}
		w.ln("\tint index(%s p) const {", gpuIds[g.dims])
		w.ln("\t\treturn %s;", indexExpr(g, "p"))
		w.ln("\t}")
		ret := g.ret.glsl
		if ret == "" {
			ret = "void"
		}
		if u.hasCPU(c, f) {
			w.gpu(g.s.Body, "", fmt.Sprintf("\t%s body(Id id) const {", ret), "}", structs)
		} else {
			w.ln("\t%s body(Id) const { // It uses barrier() or shared variables, which have no CPU fallback.", ret)
			if ret != "void" {
				w.ln("\t\treturn {};")
			}
			w.ln("\t}")
		}
		w.ln("\tstatic gdpp::gpu::Kernel &_gdpp_kernel() {")
		w.ln("\t\tstatic gdpp::gpu::Kernel kernel(%q, glsl, { %d, %d, %d }, %t, %t, %t);", c.name+"."+g.s.Name,
			g.group[0], g.group[1], g.group[2], g.main, u.hasCPU(c, f), !g.async)
		w.ln("\t\treturn kernel;")
		w.ln("\t}")
		w.ln("};")
	}
	if c.compilesShaders() {
		w.ln("")
		w.ln("// The class's shaders, which @factory_shader's methods compile.")
		w.ln("inline const gdpp::gpu::Shaders _gdpp_shaders = {")
		for _, f := range shaders {
			w.ln("\t{ %q, &%s::_gdpp_kernel },", f.gpu.s.Name, shaderStruct(f))
		}
		w.ln("};")
	}
	w.ln("")
	w.ln("} // namespace %s", shaderNamespace(c))
}

// indexExpr returns the flat index of the cell p in the grid of g, as GLSL and C++.
func indexExpr(g *gpuModel, p string) string {
	grid := g.params[g.grid].p.Name
	switch g.dims {
	case 1:
		return p
	case 2:
		return fmt.Sprintf("%s.y * %s.x + %s.x", p, grid, p)
	}
	return fmt.Sprintf("(%s.z * %s.y + %s.y) * %s.x + %s.x", p, grid, p, grid, p)
}

// cpuType returns the C++ type of p, a shader's parameter, on the CPU.
func cpuType(p *gpuParam) string {
	switch p.kind {
	case "buffer":
		return "gdpp::gpu::ReadBuffer<" + p.glsl + ">"
	case "array":
		return "gdpp::gpu::RwBuffer<" + p.glsl + ">"
	}
	return p.glsl
}

// glslQualified returns the C++ name of a GLSL type outside of the shaders' namespace, e.g. gdpp::glsl::vec3, or float.
func glslQualified(t string) string {
	if t == "int" || t == "float" || t == "bool" {
		return t
	}
	return "gdpp::glsl::" + t
}

// glslString returns s as a C++ string literal.
func glslString(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`).Replace(s) + `"`
}

// glsl returns the GLSL of shader f of class c: its parameters, bound like gdpp::gpu::Call binds them, the class's
// shader blocks, its body, and main, which runs the body for each cell of the grid and stores what it returns.
func (u *unit) glsl(c *classModel, f *funcModel) string {
	g := f.gpu
	var sb strings.Builder
	ln := func(format string, args ...any) { fmt.Fprintf(&sb, format+"\n", args...) }
	source := u.file.Pos.Filename
	ln("#version 450")
	ln("#extension GL_GOOGLE_cpp_style_line_directive : require")
	ln("layout(local_size_x = %d, local_size_y = %d, local_size_z = %d) in;", g.group[0], g.group[1], g.group[2])
	ln("layout(set = 0, binding = 0, std140) uniform _gdpp_Uniforms {")
	ln("\t%s %s;", g.params[g.grid].glsl, g.params[g.grid].p.Name) // First, as gdpp::gpu::Call puts it.
	for _, p := range g.params {
		if p.kind == "uniform" {
			ln("\t%s %s;", p.glsl, p.p.Name)
		}
	}
	ln("};")
	binding := 1
	for _, p := range g.params {
		switch p.kind {
		case "buffer":
			ln("layout(set = 0, binding = %d, std430) restrict readonly buffer _gdpp_Buffer%d {", binding, binding)
			ln("\t%s %s[];", p.glsl, p.p.Name)
			ln("};")
		case "array":
			ln("layout(set = 0, binding = %d, std430) buffer _gdpp_Buffer%d {", binding, binding)
			ln("\t%s %s[];", p.glsl, p.p.Name)
			ln("};")
		case "sampler":
			ln("layout(set = 0, binding = %d) uniform sampler2D %s;", binding, p.p.Name)
		case "image":
			ln("layout(set = 0, binding = %d, %s) uniform image2D %s;", binding, glslFormats[p.format], p.p.Name)
		default:
			continue
		}
		binding++
	}
	switch g.ret.kind {
	case "buffer", "array":
		ln("layout(set = 0, binding = %d, std430) restrict writeonly buffer _gdpp_Result {", binding)
		ln("\t%s _gdpp_result[];", g.ret.glsl)
		ln("};")
	case "image", "texture":
		ln("layout(set = 0, binding = %d, %s) uniform restrict writeonly image2D _gdpp_result;", binding, glslFormats[g.ret.format])
	}
	id := gpuIds[g.dims]
	ln("int index(%s p) {", id)
	ln("\treturn %s;", indexExpr(g, "p"))
	ln("}")
	for _, lib := range u.shaderBlocks(c) {
		ln("#line %d %q", lib.b.TextPos.Line, lib.file)
		ln("%s", lib.b.Text)
	}
	ret := g.ret.glsl
	if ret == "" {
		ret = "void"
	}
	ln("%s _gdpp_body(%s id) {", ret, id)
	ln("#line %d %q", g.s.Body.TextPos.Line, source)
	ln("%s", g.s.Body.Text)
	ln("}")
	ln("void main() {")
	grid := g.params[g.grid].p.Name
	switch g.dims {
	case 1:
		ln("\tint id = int(gl_GlobalInvocationID.x);")
		ln("\tif (id >= %s) {", grid)
	default:
		ln("\t%s id = %s(gl_GlobalInvocationID%s);", id, id, map[int]string{2: ".xy", 3: ""}[g.dims])
		ln("\tif (any(greaterThanEqual(id, %s))) {", grid)
	}
	ln("\t\treturn;")
	ln("\t}")
	switch g.ret.kind {
	case "void":
		ln("\t_gdpp_body(id);")
	case "buffer", "array":
		ln("\t_gdpp_result[index(id)] = _gdpp_body(id);")
	default:
		pixel := map[string]string{"float": "vec4(_gdpp_body(id), 0.0, 0.0, 1.0)", "vec2": "vec4(_gdpp_body(id), 0.0, 1.0)", "vec3": "vec4(_gdpp_body(id), 1.0)",
			"vec4": "_gdpp_body(id)"}[g.ret.glsl]
		ln("\timageStore(_gdpp_result, id, %s);", pixel)
	}
	ln("}")
	return sb.String()
}

// compilesShaders reports whether class c has a method of @factory_shader, which needs the list of its shaders.
func (c *classModel) compilesShaders() bool {
	return slices.ContainsFunc(factoryAnnotations["factory_shader"], func(role string) bool { return c.factory[role] != "" })
}

// compileCall returns the call of the runtime that a method of @factory_shader for role makes, without the task that
// the async and detached ones start.
func compileCall(c *classModel, role string) string {
	if strings.HasPrefix(role, "compile_shaders") {
		return fmt.Sprintf("gdpp::gpu::compile(::%s::_gdpp_shaders)", shaderNamespace(c))
	}
	return fmt.Sprintf("gdpp::gpu::compile(::%s::_gdpp_shaders, %q, p_shader)", shaderNamespace(c), c.name)
}

// shaderCall writes the body of the generated function of shader f of class c, which runs it with its arguments: on
// the GPU, or else on the CPU, with the C++ struct of its body.
func (u *unit) shaderCall(w *writer, c *classModel, f *funcModel) {
	g := f.gpu
	kernel := "::" + shaderNamespace(c) + "::" + shaderStruct(f)
	w.ln("\tgdpp::gpu::Call _gdpp_call(%s::_gdpp_kernel(), %s);", kernel, g.params[g.grid].p.Name)
	var members []string
	for _, p := range g.params {
		name := p.p.Name
		switch p.kind {
		case "grid", "uniform":
			if p.kind == "uniform" {
				w.ln("\t_gdpp_call.uniform(%s);", name)
			}
			members = append(members, fmt.Sprintf("%s(%s)", glslQualified(p.glsl), name))
		case "buffer":
			w.ln("\t_gdpp_call.input(%s);", name)
			members = append(members, fmt.Sprintf("gdpp::gpu::ReadBuffer<%s>(%s)", glslQualified(p.glsl), name))
		case "array":
			w.ln("\t_gdpp_call.array(%s);", name)
			members = append(members, fmt.Sprintf("gdpp::gpu::RwBuffer<%s>(%s)", glslQualified(p.glsl), name))
		case "sampler":
			w.ln("\t_gdpp_call.sampler(%s);", name)
			members = append(members, fmt.Sprintf("gdpp::gpu::sampled(%s)", name))
		case "image":
			w.ln("\t_gdpp_call.image(%s, gdpp::GpuFormat::%s);", name, p.format)
			members = append(members, fmt.Sprintf("gdpp::gpu::writable(%s, gdpp::GpuFormat::%s)", name, p.format))
		}
	}
	make := fmt.Sprintf("[&] { return %s{ %s }; }", kernel, strings.Join(members, ", "))
	switch g.ret.kind {
	case "void":
		w.ln("\t_gdpp_call.run(%s);", make)
	case "buffer":
		w.ln("\treturn _gdpp_call.result_buffer<%s>(%s);", g.ret.cpp, make)
	case "array":
		w.ln("\treturn _gdpp_call.result_array<%s>(%s);", g.ret.cpp, make)
	case "image":
		w.ln("\treturn _gdpp_call.result_image(gdpp::GpuFormat::%s, %s);", g.ret.format, make)
	case "texture":
		w.ln("\treturn _gdpp_call.result_texture(gdpp::GpuFormat::%s, %s);", g.ret.format, make)
	}
}

// gpuNames are the names of the GPU runtime that C++ code may use.
var gpuNames = []string{"GpuArray", "GpuFormat", "gpu_texture"}

// usesGpuNames reports whether the GD++ source src names the GPU runtime anywhere, e.g. in C++ code.
func usesGpuNames(src string) bool {
	return slices.ContainsFunc(identifiers(src), func(id string) bool { return slices.Contains(gpuNames, id) })
}

// usesGpu reports whether the unit needs the GPU runtime: it has shaders, or uses GpuArray types or other names of
// the GPU runtime.
func (u *unit) usesGpu() bool {
	if usesGpuNames(u.src) {
		return true
	}
	uses := func(funcs []*funcModel) bool {
		return slices.ContainsFunc(funcs, func(f *funcModel) bool {
			return f.gpu != nil || f.ret.gpu != "" || f.ret.async != nil && f.ret.async.gpu != "" ||
				slices.ContainsFunc(f.params, func(t *gtype) bool { return t.gpu != "" || t.async != nil && t.async.gpu != "" })
		})
	}
	for _, c := range u.classes {
		if uses(c.funcs) || len(c.shaders) > 0 || slices.ContainsFunc(c.vars, func(v *varModel) bool { return v.t.gpu != "" }) ||
			slices.ContainsFunc(c.signals, func(s *signalModel) bool {
				return slices.ContainsFunc(s.params, func(t *gtype) bool { return t.gpu != "" })
			}) {
			return true
		}
	}
	for _, e := range u.externs {
		if uses(e.funcs) || slices.ContainsFunc(e.vars, func(v *varModel) bool { return v.t.gpu != "" }) {
			return true
		}
	}
	for _, t := range u.traits {
		if uses(t.funcs) {
			return true
		}
	}
	return false
}

// usesGpuTypes reports whether class c needs the package's GPU classes: it has shaders, or uses GpuArray types.
func usesGpuTypes(c *Class) bool {
	isGpu := func(t *Type) bool {
		if t == nil {
			return false
		}
		if t.Name == "GpuArray" {
			return true
		}
		return slices.ContainsFunc(t.Args, func(a *Type) bool { return a.Name == "GpuArray" })
	}
	hasGpu := func(params []*Param) bool {
		return slices.ContainsFunc(params, func(p *Param) bool { return isGpu(p.Type) })
	}
	return slices.ContainsFunc(c.Members, func(m *Member) bool {
		switch {
		case m.Shader != nil:
			return true
		case m.Func != nil:
			return isGpu(m.Func.Return) || hasGpu(m.Func.Params)
		case m.Signal != nil:
			return hasGpu(m.Signal.Params)
		case m.Var != nil:
			return isGpu(m.Var.Type)
		}
		return false
	})
}

// swizzleRegexp matches a swizzle of 2 to 4 components, all from one of GLSL's sets.
var swizzleRegexp = regexp.MustCompile(`^(?:[xyzw]{2,4}|[rgba]{2,4}|[stpq]{2,4})$`)

// glslDropped are GLSL's qualifiers that C++ has no use for.
var glslDropped = map[string]bool{"in": true, "highp": true, "mediump": true, "lowp": true, "precise": true, "coherent": true,
	"restrict": true, "readonly": true, "writeonly": true}

// glslToCpp turns GLSL code, a shader's body or shader block, into C++ that gdpp::glsl compiles, for the CPU. It
// keeps every line where it is. structs are the names of the structs that the class's shader blocks declare.
//   - a swizzle of 2 to 4 components, e.g. `v.xy`, becomes `v.swizzle<0, 1>()`, or `v.swizzle_ref<0, 1>()` where
//     it's assigned to,
//   - `out T x` and `inout T x` become `T &x`, and `in`, precision and memory qualifiers are dropped,
//   - `not(v)` becomes `glsl_not(v)`, since not is a C++ keyword, and `shared` becomes `static`,
//   - a struct's constructor, `S(a, b)`, becomes `S{a, b}`.
func glslToCpp(code string, structs []string) string {
	lex, err := gdppLexer.LexString("", code)
	if err != nil {
		return code
	}
	var ts []lexer.Token
	for {
		t, err := lex.Next()
		if err != nil || t.EOF() {
			break
		}
		ts = append(ts, t)
	}
	out := make([]string, len(ts))
	for i, t := range ts {
		out[i] = t.Value
	}
	// dropSpace drops the spaces after ts[i], keeping their newlines.
	dropSpace := func(i int) {
		for k := i + 1; k < len(ts) && (ts[k].Type == tokWhitespace || ts[k].Type == tokNewline); k++ {
			out[k] = strings.Repeat("\n", strings.Count(ts[k].Value, "\n"))
		}
	}
	for i, t := range ts {
		if t.Type != tokIdent {
			continue
		}
		member := isPunct(at(ts, prevToken(ts, i)), ".")
		switch {
		case member && swizzleRegexp.MatchString(t.Value):
			var idx []string
			for _, r := range t.Value {
				idx = append(idx, strconv.Itoa(strings.IndexRune("xyzw", r)+strings.IndexRune("rgba", r)+strings.IndexRune("stpq", r)+2))
			}
			method := "swizzle"
			if j := skipSpace(ts, i+1); j < len(ts) && assigns(ts, j) {
				method = "swizzle_ref"
			}
			out[i] = method + "<" + strings.Join(idx, ", ") + ">()"
		case member:
		case t.Value == "out" || t.Value == "inout":
			typ := skipSpace(ts, i+1)
			name := skipSpace(ts, typ+1)
			if typ < len(ts) && ts[typ].Type == tokIdent && name < len(ts) && ts[name].Type == tokIdent {
				out[i] = ""
				dropSpace(i)
				out[name] = "&" + out[name]
			}
		case glslDropped[t.Value]:
			out[i] = ""
			dropSpace(i)
		case t.Value == "shared":
			out[i] = "static"
		case t.Value == "not":
			if j := skipSpace(ts, i+1); j < len(ts) && isPunct(ts[j], "(") {
				out[i] = "glsl_not"
			}
		case slices.Contains(structs, t.Value):
			j := skipSpace(ts, i+1)
			if p := prevToken(ts, i); j < len(ts) && isPunct(ts[j], "(") && !(p >= 0 && ts[p].Type == tokIdent && ts[p].Value == "struct") {
				if end := closing(ts, j); end < len(ts) {
					out[j], out[end] = "{", "}"
				}
			}
		}
	}
	return strings.Join(out, "")
}

// at returns ts[i], or an empty token if i is out of range.
func atToken(ts []lexer.Token, i int) lexer.Token {
	if i < 0 || i >= len(ts) {
		return lexer.Token{}
	}
	return ts[i]
}

// assigns reports whether ts[j] starts an assignment: `=` but not `==`, or a compound one, e.g. `+=`.
func assigns(ts []lexer.Token, j int) bool {
	next := atToken(ts, j+1)
	switch {
	case isPunct(ts[j], "="):
		return !isPunct(next, "=")
	case ts[j].Type == tokPunct && strings.Contains("+-*/%&|^", ts[j].Value):
		return isPunct(next, "=")
	}
	return false
}

// gpu writes b, GLSL code, as C++ for the CPU, like block does for C++ code. source is how #line names b's file, if
// it's not the writer's.
func (w *writer) gpu(b *Block, source, prefix, suffix string, structs []string) {
	line := b.TextPos.Line
	source = cmpOr(source, w.source)
	if b.Origin.Source != "" {
		line, source = b.Origin.Line, b.Origin.Source
	}
	w.ln("#line %d %q", line, source)
	w.ln("%s", prefix+glslToCpp(b.Text, structs)+suffix)
	w.ln("#line %d %q", w.lines+2, w.self)
}

// documentGpuArrayClass returns the Godot XML documentation of the class of GPU arrays named name.
func documentGpuArrayClass(name string) string {
	x := &xmlWriter{}
	x.ln(0, `<?xml version="1.0" encoding="UTF-8" ?>`)
	x.ln(0, fmt.Sprintf(`<class name="%s" inherits="RefCounted" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance" xsi:noNamespaceSchemaLocation="https://raw.githubusercontent.com/godotengine/godot/master/doc/class.xsd">`,
		xmlEscape(name, true)))
	x.ln(1, "<brief_description>")
	x.ln(2, "An array of floats, ints, vectors or colors that stays on the GPU, where shaders read and write it.")
	x.ln(1, "</brief_description>")
	x.ln(1, "<description>")
	for _, line := range strings.Split(`GD++ provides this class: its runtime defines it, and the package registers it. Run [code]gd++ man shaders[/code] to read how to use it.
A GPU array holds the elements of one kind of packed array: [PackedFloat32Array], [PackedInt32Array], [PackedVector2Array], [PackedVector3Array], [PackedVector4Array] or [PackedColorArray]. Shaders read and write its elements in place, so a simulation can run many steps without copying them back and forth. [method read] copies them back, once you need them.
Without a GPU, e.g. with the Compatibility renderer or [code]--headless[/code], it's an array in memory, and shaders run on the CPU.
[codeblock]
var heights = GameGpuArray.from(PackedFloat32Array([0.0, 1.0, 2.0]))
Terrain.erode(heights.size(), heights, 0.1)
print(heights.read())
[/codeblock]
In GD++ code, the type [code]GpuArray[T][/code] names it, e.g. [code]GpuArray[float][/code]. Each package with shaders or GpuArray types has its own class of GPU arrays, named after its prefix, e.g. [code]FooGpuArray[/code].`, "\n") {
		x.ln(2, line)
	}
	x.ln(1, "</description>")
	x.ln(1, "<tutorials>")
	x.ln(1, "</tutorials>")
	x.ln(1, "<methods>")
	for _, m := range []struct{ name, ret, qualifiers, arg, doc string }{
		{"from", "Variant", "static", "packed", "Returns a new GPU array with the elements of [param packed]: a [PackedFloat32Array], [PackedInt32Array], [PackedVector2Array], [PackedVector3Array], [PackedVector4Array] or [PackedColorArray]. Its element type is that of [param packed]."},
		{"read", "Variant", "", "", "Returns the elements as a packed array of the GPU array's element type. It waits until the shaders that write them are done."},
		{"size", "int", "", "", "Returns how many elements the GPU array has."},
		{"write", "void", "", "packed", "Sets the elements from [param packed], a packed array of the GPU array's element type and size."},
	} {
		if m.qualifiers != "" {
			x.ln(2, fmt.Sprintf(`<method name="%s" qualifiers="%s">`, m.name, m.qualifiers))
		} else {
			x.ln(2, fmt.Sprintf(`<method name="%s">`, m.name))
		}
		x.ln(3, fmt.Sprintf(`<return type="%s" />`, m.ret))
		if m.arg != "" {
			x.ln(3, fmt.Sprintf(`<param index="0" name="%s" type="Variant" />`, m.arg))
		}
		x.ln(3, "<description>")
		x.ln(4, m.doc)
		x.ln(3, "</description>")
		x.ln(2, "</method>")
	}
	x.ln(1, "</methods>")
	x.ln(0, "</class>")
	return x.sb.String()
}
