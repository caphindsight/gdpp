// Lambdas right after a macro call, and after an object-like macro.
#define FIRST(x) (void)(x),
#define DECAY +
auto g = (FIRST(1) [&]() -> int {
	assert true;
	return 1;
});
auto h = DECAY [](int x) -> int { assert x; return x; };
assert g() == h(1);
#undef FIRST
#undef DECAY
