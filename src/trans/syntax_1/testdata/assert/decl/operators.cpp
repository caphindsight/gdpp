// Operators, which look like neither functions nor lambdas.
int data[4] = {};
int &operator[](int i) {
	assert i >= 0 && i < 4;
	return data[i];
}
explicit operator bool() const {
	assert data[0] >= 0;
	return data[0] != 0;
}
bool operator()(int a, int b) const { assert a != b; return a < b; }
void *operator new[](std::size_t n) { assert n > 0; return ::operator new(n); }
void operator delete[](void *p) { assert p; ::operator delete(p); }
