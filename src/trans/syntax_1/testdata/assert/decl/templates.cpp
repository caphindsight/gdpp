// Templates, attributes and constraints before functions and constructors.
template <typename T>
[[nodiscard]] T clamp_to(T v, T lo, T hi) {
	assert lo <= hi;
	return v < lo ? lo : v > hi ? hi : v;
}
template <class T>
void each(const std::vector<T> &items, const std::function<void(const T &)> &f) {
	for (const auto &item : items) {
		assert &item != nullptr;
		f(item);
	}
}
struct Box {
	template <typename T>
	Box(T v) requires std::is_integral_v<T> : size(int(v)) { assert v >= 0; }
	int size;
};
