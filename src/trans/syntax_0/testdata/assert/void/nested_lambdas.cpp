// A lambda that returns a lambda, which holds a void lambda.
auto make = [](int base) -> std::function<int(int)> {
	assert base >= 0;
	return [base](int x) -> int {
		assert x != base;
		auto check = [&] {
			assert x > 0;
			if (x > 100) {
				return;
			}
		};
		check();
		return x + base;
	};
};
assert make(1)(2) == 3;
