// Recursive lambdas, through std::function and through a parameter.
std::function<int(int)> fib = [&](int n) -> int {
	assert n >= 0;
	return n < 2 ? n : fib(n - 1) + fib(n - 2);
};
auto walk = [&](auto &self, int depth) -> void {
	assert depth < 10;
	if (depth < 3) self(self, depth + 1);
};
walk(walk, 0);
return fib(5);
