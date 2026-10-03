// Return types that are hard to read: templates, pointers, references, and specifiers before them.
auto pair = [](int a) -> std::pair<int, std::vector<int>> { assert a; return {a, {}}; };
auto ptr = [](int *p) -> int * { assert p; return p; };
auto ref = [](int &r) -> int & { assert r; return r; };
auto vp = [](void *p) -> void * { assert p; return p; };
auto vv = [](int x) -> void { assert x; };
auto qual = [](int x) mutable noexcept -> std::size_t { assert x; return 0; };
auto tmpl = []<typename A, typename B>(A a, B b) -> bool { assert a == b; return true; };
int one = 1;
assert pair(1).first == *ptr(&one) && ref(one) == 1 && vp(&one) && qual(1) == 0 && tmpl(1, 1);
vv(1);
