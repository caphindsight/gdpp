// Lambdas that return values, passed to algorithms from a function that returns void.
std::vector<int> v = {3, 1, 2};
assert !v.empty();
std::sort(v.begin(), v.end(), [](int a, int b) -> bool {
	assert a != b;
	return a < b;
});
auto sum = std::accumulate(v.begin(), v.end(), 0, [&](int acc, int x) -> int { assert(x > 0); return acc + x; });
std::for_each(v.begin(), v.end(), [&](int x) { assert x <= sum; });
assert sum == 6;
