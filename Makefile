# board — single-binary shared whiteboard

BINARY := board
LDFLAGS := -s -w

.PHONY: run dev build check test smoke shot status caps dist clean

run: build          ## build and serve with the embedded UI
	./$(BINARY) serve

dev: build          ## serve the UI from disk, so edits need no rebuild
	./$(BINARY) serve --dev

build:              ## build the binary for this machine
	go build -o $(BINARY) ./cmd/board

check:              ## vet + gofmt check
	go vet ./...
	@out=$$(gofmt -l .); if [ -n "$$out" ]; then echo "gofmt needed:"; echo "$$out"; exit 1; fi
	@echo "check ok"

test:               ## the Go tests
	go test -race ./...

smoke:              ## headless browser smoke suite (server must be running)
	./test/smoke.sh

shot:               ## screenshot tabs into .board/run/shots/
	./test/shot.sh

status: build       ## what is running for this project, and on which port
	./$(BINARY) status

dist:               ## cross-compile release binaries into dist/
	@mkdir -p dist
	@for t in linux/amd64 linux/arm64 darwin/amd64 darwin/arm64 windows/amd64; do \
		os=$${t%/*}; arch=$${t#*/}; ext=""; \
		[ "$$os" = "windows" ] && ext=".exe"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build -ldflags="$(LDFLAGS)" \
			-o dist/$(BINARY)-$$os-$$arch$$ext ./cmd/board && echo "  dist/$(BINARY)-$$os-$$arch$$ext"; \
	done

clean:
	rm -rf $(BINARY) dist

# Regenerate what the binary emits from its own manifest: the skill's reference,
# and the control module the renderers import. The authored files carry judgment;
# these carry facts, and facts drift.
#
# Built TWICE on purpose. pkg/aboard/web is embedded, so the first binary emits
# the module from the current specs and the second one embeds the module it just
# wrote — otherwise the server keeps serving the previous copy and the change
# appears to do nothing, which is this repo's oldest gotcha.
caps: build
	./$(BINARY) capabilities --format js > pkg/aboard/web/views/controls.generated.js
	./$(BINARY) capabilities --format md > .claude/skills/board/references/reference.generated.md
	go build -o $(BINARY) ./cmd/board
	@./$(BINARY) capabilities --check
