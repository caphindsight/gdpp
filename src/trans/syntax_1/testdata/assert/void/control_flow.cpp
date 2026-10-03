// Assertions in every kind of statement.
int n = 3;
switch (n) {
case 1: assert n == 1; break;
case 3: {
	assert n > 2;
	break;
}
default:
	assert(false);
}
do {
	assert n >= 0;
} while (--n > 0);
if (n) assert n; else assert !n;
for (int i = 0; i < 2; i++)
	assert i < 2;
while (n < 1) {
	if (++n) { assert n == 1; continue; }
}
