// A local class: its constructor, destructor and methods return differently from the function around it.
struct Counter {
	int count;
	Counter(int start) : count(start) {
		assert start >= 0;
	}
	~Counter() {
		assert count >= 0;
	}
	int next() {
		assert count < 100;
		return ++count;
	}
	void reset() { assert count != 0; count = 0; }
};
Counter c{1};
assert c.next() == 2;
c.reset();
