// Lambdas in ternaries and in assertions' conditions, next to subscripts.
std::map<int, int> m{{1, 2}};
auto pick = true ? +[](int x) -> int { assert x; return x; } : +[](int x) -> int { assert !x; return 0; };
assert std::all_of(m.begin(), m.end(), [](auto &kv) { return kv.second > kv.first; });
assert ([&] { return m[1] == 2; })();
return pick(m[1]) + m.at(1);
