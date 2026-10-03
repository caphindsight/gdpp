// Functions whose return type C++ deduces: those that return a value fail with an explanation.
auto twice(int x) {
	assert x > 0;
	return x * 2;
}

decltype(auto) first(std::vector<int> &v) {
	assert !v.empty();
	return v[0];
}

auto nothing(int x) {
	assert x;
}
