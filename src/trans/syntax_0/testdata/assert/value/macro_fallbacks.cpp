// A void lambda that a macro writes, in a function that returns a value.
#define EACH(list) for (auto &item : list) [&]
std::vector<int> items{1, 2};
int total = 0;
EACH(items) { assert_void item > 0; total += item; }();
#undef EACH
return total;
