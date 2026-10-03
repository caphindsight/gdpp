// A class's members: functions, a nested class with constructors and a destructor, and lambdas in initializers.
struct Helper {
	int value;
	explicit Helper(int v) : value(v) { assert v >= 0; }
	Helper(const Helper &other) : value{other.value} {
		assert other.value >= 0;
	}
	~Helper() { assert value >= 0; }
	int get() const { assert value; return value; }
};
int cached = 0;
std::function<int(int)> scale = [](int x) -> int { assert x; return x * 2; };
std::function<void()> ping = [] { assert true; };

public:
Asserted() : cached{1} {
	assert cached == 1;
}
~Asserted() noexcept {
	assert cached >= 0;
}
int compute(int x) {
	assert x > 0;
	return x * Helper(x).get();
}
void clear() noexcept {
	assert cached >= 0;
	cached = 0;
}
