// Lambdas called right away, to initialize values.
const int limit = [&]() -> int {
	assert true;
	return 5;
}();
[&] { assert limit == 5; }();
const auto msg = std::string([&]() -> const char * { assert limit; return "ok"; }());
assert msg.size() == 2;
