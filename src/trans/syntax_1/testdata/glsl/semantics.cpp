// Checks that GLSL in C++, which runs shaders on the CPU, works like GLSL. TestGlsl compiles and runs it.

#include <gd++/syntax_1_gpu.hpp>
#include <cstdio>
using namespace gdpp::glsl;
int fails = 0;
#define CHECK(c) do { if (!(c)) { std::printf("FAIL %s:%d %s\n", __FILE__, __LINE__, #c); fails++; } } while (0)
bool near(float a, float b) { return std::fabs(a - b) < 1e-5f; }
int main() {
	vec4 v(1.0, 2.0, 3.0, 4.0);
	v.swizzle_ref<'x', 'y'>() = vec2(5.0, 6.0);
	CHECK(v.x == 5 && v.y == 6 && v.z == 3);
	v.swizzle_ref<'w', 'z'>() += vec2(1.0);
	CHECK(v.w == 5 && v.z == 4);
	CHECK(v.r == v.x && v.a == v.w && v.s == v.x);
	vec2 w = v.swizzle<'b', 'r'>();
	CHECK(w.x == 4 && w.y == 5);
	CHECK(near(mod(-1.0f, 3.0f), 2.0f));
	CHECK(near(mix(0.0f, 10.0f, 0.25f), 2.5f));
	CHECK(mix(vec2(0.0), vec2(10.0), 0.5) == vec2(5.0));
	CHECK(clamp(5, 0, 3) == 3);
	CHECK(near(clamp(0.5, 0.0, 1.0), 0.5f));
	mat2 m(1.0, 2.0, 3.0, 4.0); // Columns (1, 2) and (3, 4).
	vec2 r = m * vec2(1.0, 1.0);
	CHECK(r.x == 4 && r.y == 6);
	vec2 l = vec2(1.0, 1.0) * m;
	CHECK(l.x == 3 && l.y == 7);
	mat2 i = inverse(m) * m;
	CHECK(near(i[0][0], 1) && near(i[1][1], 1) && near(i[0][1], 0) && near(i[1][0], 0));
	CHECK(near(determinant(mat3(2.0)), 8.0f));
	CHECK(cross(vec3(1, 0, 0), vec3(0, 1, 0)) == vec3(0, 0, 1));
	CHECK(near(length(vec3(3, 4, 0)), 5.0f));
	CHECK(all(lessThan(ivec2(1, 2), ivec2(2, 3))) && !any(glsl_not(bvec2(true, true))));
	CHECK(abs(-3) == 3 && sign(-2.5f) == -1.0f);
	CHECK(near(smoothstep(0.0, 1.0, 0.5), 0.5f));
	CHECK(step(0.5, vec2(0.2, 0.8)) == vec2(0.0, 1.0));
	CHECK(near(float_from_half(half_from_float(1.5f)), 1.5f));
	CHECK(unpackHalf2x16(packHalf2x16(vec2(0.25, -2.0))) == vec2(0.25, -2.0));
	int n = 1;
	CHECK(atomicAdd(n, 2) == 1 && n == 3);
	CHECK(atomicMax(n, 7) == 3 && n == 7);
	ivec3 a(7, 8, 9);
	CHECK((a % 4) == ivec3(3, 0, 1));
	vec3 f = ivec3(1, 2, 3); // ints convert to floats implicitly, as in GLSL.
	CHECK(f == vec3(1.0, 2.0, 3.0));
	CHECK(findMSB(8) == 3 && findLSB(8) == 3 && bitCount(7u) == 3);
	std::printf(fails ? "FAILED\n" : "OK\n");
	return fails;
}
