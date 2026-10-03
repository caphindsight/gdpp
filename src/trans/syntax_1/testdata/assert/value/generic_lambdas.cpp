// Generic lambdas, with auto parameters.
auto show = [](const auto &x) { assert x.size() > 0; };
show(std::string("a"));
auto len = [](const auto &x) -> std::size_t { assert !x.empty(); return x.size(); };
assert len(std::string("ab")) == 2;
return int(len(std::vector<int>{1}));
