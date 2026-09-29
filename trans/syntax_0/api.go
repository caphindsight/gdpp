package syntax_0

import (
	_ "embed"
	"fmt"

	"gd++/trans/meta"
)

// RuntimeHeaderName is how generated headers include RuntimeHeader.
const RuntimeHeaderName = "gd++/syntax_0.hpp"

// RuntimeHeader holds the helpers that all generated C++ uses. It doesn't depend on any GD++ file.
//
//go:embed runtime.hpp
var RuntimeHeader string

// ListClasses returns the classes, externs and enum types that the GD++ source src declares.
func ListClasses(filename, src string) ([]meta.Declaration, error) {
	u, err := parseUnit(filename, src)
	if err != nil {
		return nil, err
	}
	return u.declarations()
}

// GenerateHeader returns the C++ header for the GD++ source src.
func GenerateHeader(filename, src string, opts meta.Options) (string, error) {
	u, err := newUnit(filename, src, opts)
	if err != nil {
		return "", err
	}
	return u.header(), nil
}

// GenerateSource returns the C++ source file for the GD++ source src.
func GenerateSource(filename, src string, opts meta.Options) (string, error) {
	u, err := newUnit(filename, src, opts)
	if err != nil {
		return "", err
	}
	return u.source(), nil
}

// DocumentClass returns the Godot XML documentation of the class named class in the GD++ source src.
func DocumentClass(filename, src, class string, opts meta.Options) (string, error) {
	u, err := newUnit(filename, src, opts)
	if err != nil {
		return "", err
	}
	for _, c := range u.classes {
		if c.name == class {
			return u.document(c), nil
		}
	}
	return "", fmt.Errorf("There is no class %s in %s.", class, filename)
}
