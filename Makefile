BINARY_NAME=gd++
PREFIX?=/usr/local
BINDIR=$(PREFIX)/bin

.PHONY: build install clean readme test

build:
	go -C src build -o ../$(BINARY_NAME)

install: build
	mkdir -p $(DESTDIR)$(BINDIR)
	cp $(BINARY_NAME) $(DESTDIR)$(BINDIR)/$(BINARY_NAME)

clean:
	rm -f $(BINARY_NAME)

readme: build
	GDPP=$(CURDIR)/$(BINARY_NAME) readme/render.sh

# TestCompile also runs when GDPP_GODOT_CPP names a godot-cpp checkout with generated bindings.
test:
	go -C src vet ./...
	go -C src test ./...
