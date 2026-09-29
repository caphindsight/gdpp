{{define "register_classes"}}
template <typename T>
constexpr bool gdpp_is_package_class = false{{range .Classes}} || std::is_same_v<T, {{.}}>{{end}};

// Runtime classes are GD++ classes without @tool: in the editor, Godot makes
// placeholders of them that store their properties but run none of their code.
template <typename T>
constexpr bool gdpp_is_runtime_class = false{{range .RuntimeClasses}} || std::is_same_v<T, {{.}}>{{end}};

// Registers T, after its parent if that's a class of this package too, since
// Godot requires parents to be registered first.
template <typename T>
static void gdpp_register_class() {
	static bool registered = false;
	if (registered) {
		return;
	}
	registered = true;
	if constexpr (gdpp_is_package_class<typename T::parent_type>) {
		gdpp_register_class<typename T::parent_type>();
	}
	if constexpr (gdpp_is_runtime_class<T>) {
		GDREGISTER_RUNTIME_CLASS(T);
	} else {
		GDREGISTER_CLASS(T);
	}
}
{{end}}
