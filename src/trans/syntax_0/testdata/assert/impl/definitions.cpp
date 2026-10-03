// Definitions outside of a class: constructors, a destructor, methods, and functions with trailing return types.
namespace {
struct Grid {
	Grid(int w);
	~Grid();
	int at(int i) const;
	std::vector<int> cells;
};
}

Grid::Grid(int w) : cells(w) {
	assert w > 0;
}

Grid::~Grid() {
	assert !cells.empty();
}

int Grid::at(int i) const {
	assert i < int(cells.size());
	return cells[i];
}

static void fill(Grid &g, int v) {
	assert v != 0;
	for (auto &c : g.cells) c = v;
}

auto area(const Grid &g) -> int {
	assert !g.cells.empty();
	return int(g.cells.size());
}

const auto table = [] {
	std::array<int, 3> t{};
	for (int i = 0; i < 3; i++) t[i] = i;
	return t;
}();
