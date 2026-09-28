BINARY_NAME=gd++
PREFIX?=/usr/local
BINDIR=$(PREFIX)/bin

.PHONY: build install clean

build:
	go build -o $(BINARY_NAME)

install: build
	mkdir -p $(DESTDIR)$(BINDIR)
	cp $(BINARY_NAME) $(DESTDIR)$(BINDIR)/$(BINARY_NAME)

clean:
	rm -f $(BINARY_NAME)
