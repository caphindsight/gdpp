// Captures with brackets, braces and calls in them.
std::vector<int> items{1, 2, 3};
auto first = [v = std::vector<int>{items[0], items[1]}, &items, n = items.size()]() mutable -> int {
	assert n == items.size();
	assert v[0] == 1;
	return v[1];
};
auto none = [x = std::map<int, int>{{1, 2}}] { assert x.at(1) == 2; };
none();
assert first() == 2;
