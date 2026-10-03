// Brackets in strings and comments, and comments in a lambda's header.
const char *s = "[&] { assert x; }";
char open = '[';
auto f = [&]() -> /* the type */ void { assert s[0] == open; };
auto g = [&]() // returns a value
	-> int {
	assert *s; // [] { }
	return 1;
};
/* [&] { assert false; } */
f();
assert g() == 1;
