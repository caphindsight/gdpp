// Subscripts, which look like lambdas' captures, before blocks, braces, macros and structured bindings.
#define IDENTITY(x) (x)
int grid[3][3] = {};
std::map<std::string, std::vector<int>> m{{"a", {1, 2}}};
assert grid[1][2] == 0;
if (m["a"][0]) {
	assert m["a"][1] == 2;
}
int arr[2] {grid[0][0], grid[2][2]};
for (auto &[key, values] : m) {
	assert !values.empty() && key[0] == 'a';
}
assert IDENTITY(arr)[1] == 0;
int *heap = new int[2]{arr[0], arr[1]};
delete[] heap;
#undef IDENTITY
