// A lambda that a macro writes: GD++ can't see it, so the fallback says what it returns.
#define CALLBACK(...) [&](__VA_ARGS__)
auto parse = CALLBACK(const char *s) -> int { assert_val s != nullptr; return s[0]; };
auto print = CALLBACK(int x) { assert_void x != 0; };
print(parse("a"));
#undef CALLBACK
