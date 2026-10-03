// Void lambdas in a function that returns a value.
int total = 0;
std::vector<std::function<void(int)>> handlers;
handlers.push_back([&](int x) { assert x >= 0; total += x; });
handlers.push_back([&](int x) -> void {
	if (x > 10) {
		assert total < 100;
		return;
	}
	total -= x;
});
for (auto &h : handlers) {
	h(1);
}
assert total == 0;
return total;
