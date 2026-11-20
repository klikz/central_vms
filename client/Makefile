# vmsagent — build/ishga tushirish qoidalari (root REJA §4.2, client REJA §5).
# Medium effort rejimi: har qadamdan keyin `make fmt vet lint race` (TASKS §0.4).

MODULE      := central-vms/client
BIN         := vmsagent
BUILD_DIR   := build
RUN_DIR     := $(BUILD_DIR)/run
GO          := go
GOFLAGS     := -trimpath
CGO         := 0
GOOS        ?= linux
GOARCH      ?= amd64

# Versiya: git bo'lmasa 'dev' (root REJA §4.3)
VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_AT  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

# LDFLAGS VERSION'dan KEYIN kelishi shart (:= darhol kengaytiradi)
LDFLAGS     := -s -w -X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.builtAt=$(BUILD_AT)

# Qulflangan bog'liqliklar (docs/deps.md). Tarmoq bo'lsa: make deps-fetch
DEPS := github.com/pion/webrtc/v4@v4.2.27 \
        github.com/pion/interceptor@v0.1.51 \
        github.com/pion/rtp@v1.10.6 \
        github.com/bluenviron/gortsplib/v4@v4.16.3 \
        github.com/bluenviron/mediacommon@v1.14.1 \
        github.com/google/uuid@v1.6.0 \
        gopkg.in/yaml.v3@v3.0.1 \
        github.com/gorilla/websocket@v1.5.3

# Coverage bo'sagasi (TASKS §0.4)
COVER_CORE := 70
COVER_ALL  := 50

# To'xtatish pattern'lari ([x] trigi - pkill o'z shell'ini oldirmasligi uchun;
# `go run` bolasi ~/.cache/go-build/... ichida bo'lgani uchun alohida pattern kerak.
STOP_PATTERNS := 'build/[v]msagent -config' 'exe/[v]msagent' 'go-build[^ ]*/[v]msagent -config' \
                 'exe/[d]evserver' 'go-build[^ ]*/[d]evserver' \
                 'go run ./cmd/[v]msagent' 'go run ./cmd/[d]evserver'

BACKUP_DIR ?= /var/backups/vmsagent

.PHONY: all start dev build run install uninstall logs logs-err backup restore status test race cover gate \ push update
        lint fmt vet fixtures deps deps-fetch devserver itest stress secretscan ui-lint ui-shot logs-err \
        package remote-update stop stop-service clean help

all: build

## Ishga tushirish: build -> lokal UI http://localhost:8082
## config/client.local.yaml bo'lsa shu ishlatiladi (lokal yo'llar, root kerak emas);
## bo'lmasa build/config/client.yaml (production yo'llari — root talab qiladi).
start: build
	@mkdir -p $(RUN_DIR)
	@CFG=$(BUILD_DIR)/config/client.yaml; \
	if [ -f config/client.local.yaml ]; then CFG=config/client.local.yaml; \
	  echo "start: config=$$CFG (lokal)"; else echo "start: config=$$CFG (production — root kerak bo'lishi mumkin; lokal uchun config/client.local.yaml yarating)"; fi; \
	./$(BUILD_DIR)/$(BIN) -config $$CFG & echo $$! > $(RUN_DIR)/$(BIN).pid; \
	echo "PID $$(cat $(RUN_DIR)/$(BIN).pid) — to'xtatish: boshqa terminalda 'make stop' (yoki Ctrl+C)"; \
	wait $$(cat $(RUN_DIR)/$(BIN).pid)

## Dev rejimi: go run + config/client.local.yaml bo'lsa shu bilan
## Tayyor binary'ni qayta qurmasdan ishga tushiradi (config/client.local.yaml bo'lsa shu)
run:
	@mkdir -p $(RUN_DIR)
	@CFG=config/client.yaml.example; if [ -f config/client.local.yaml ]; then CFG=config/client.local.yaml; fi; \
	 echo "run: binary=$(BUILD_DIR)/$(BIN) config=$$CFG"; \
	 ./$(BUILD_DIR)/$(BIN) -config $$CFG & echo $$! > $(RUN_DIR)/$(BIN).pid; \
	 wait $$(cat $(RUN_DIR)/$(BIN).pid)

dev:
	@CFG=config/client.yaml.example; \
	if [ -f config/client.local.yaml ]; then CFG=config/client.local.yaml; fi; \
	echo "dev: config = $$CFG"; \
	mkdir -p $(RUN_DIR); \
	$(GO) run ./cmd/$(BIN) -config $$CFG & echo $$! > $(RUN_DIR)/dev-$(BIN).pid; \
	wait $$(cat $(RUN_DIR)/dev-$(BIN).pid)

## build/ tarkibi root §4.1: binary + config + unit + install.sh + VERSION
build:
	@mkdir -p $(BUILD_DIR)/config $(BUILD_DIR)/systemd
	CGO_ENABLED=$(CGO) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BIN) ./cmd/$(BIN)
	cp -f config/client.yaml.example $(BUILD_DIR)/config/client.yaml
	cp -f systemd/vmsagent.service $(BUILD_DIR)/systemd/vmsagent.service
	cp -f install.sh $(BUILD_DIR)/install.sh
	printf '%s (commit %s, %s)\n' "$(VERSION)" "$(COMMIT)" "$(BUILD_AT)" > $(BUILD_DIR)/VERSION
	@echo "build/ tayyor: $$(ls $(BUILD_DIR))"

## Mock server (server kutmasdan C1–C3 ni tekshirish)
devserver:
	@mkdir -p $(RUN_DIR)
	@$(GO) run ./cmd/devserver & echo $$! > $(RUN_DIR)/devserver.pid; \
	 wait $$(cat $(RUN_DIR)/devserver.pid)

## Testlar (unit, race yo'q — tez)
test:
	$(GO) test ./... -count=1

## Race detector + coverage (majburiy tekshiruv)
race:
	$(GO) test ./... -race -count=1 -coverprofile=coverage.out -covermode=atomic

cover: race
	$(GO) tool cover -func=coverage.out | tail -1

## G1 gate: lint + race + coverage bo'sagasi
gate: fmt vet lint race
	@$(GO) run ./scripts/covergate -core $(COVER_CORE) -all $(COVER_ALL) coverage.out

lint:
	@if command -v golangci-lint >/dev/null 2>&1; then \
	  golangci-lint run ./...; \
	else \
	  echo "OG'OHLANTIRISH: golangci-lint o'rnatilmagan — go vet + gofmt bilan cheklandi" >&2; \
	  $(GO) vet ./...; \
	  test -z "$$(gofmt -l . | grep -v '^build/')" || { echo "gofmt: yuqoridagi fayllar formatlanmagan" >&2; exit 1; }; \
	fi

fmt:
	gofmt -w $$(find . -name '*.go' -not -path './build/*')

vet:
	$(GO) vet ./...

## Test fixture bazasi (SDP/annexb/encoders/CSV) — FFmpeg talab qiladi
fixtures:
	./testdata/gen.sh

## Tashqi bog'liqliklar va qulflangan versiyalar ro'yxati
deps:
	@echo "Go:          $$(go version)"; \
	echo "ffmpeg:      $$(command -v ffmpeg >/dev/null && ffmpeg -hide_banner -version | head -1 || echo 'YO'Q — kerak!')"; \
	echo "  whip muxer:  $$(ffmpeg -hide_banner -muxers 2>/dev/null | grep -c ' whip ' || echo 0) (1 bo'lishi kerak, FFmpeg >= 7.1)"; \
	echo "GPU encoders: $$(ffmpeg -hide_banner -encoders 2>/dev/null | grep -cE 'h264_(nvenc|vaapi|amf)') ta"; \
	echo; echo "Qulflangan Go modullari (docs/deps.md):"; \
	for d in $(DEPS); do echo "  $$d"; done; \
	echo; echo "Tarmoq bo'lsa: make deps-fetch"

deps-fetch:
	$(GO) get $(DEPS)
	$(GO) mod tidy

## Log/fayllarda maxfiy qiymat qidiruvi (DoD bandi)
secretscan:
	@! grep -rniE '(password|passwd|agent_secret|token)[[:space:]]*[:=][[:space:]]*"[^"$${}]+"' --include='*.go' . \
	  | grep -v '_test.go' | grep -v 'redact' | grep -v 'yamlsub' | grep .; \
	echo "secretscan: toza"

## Lokal UI statik tekshiruvi (node talab qilmaydi)
ui-lint:
	@web=internal/localui/web; \
	n=$$(grep -c 'data-screen="' $$web/index.html); \
	test $$n -ge 5 || { echo "ui-lint: ekranlar soni $$n (<5)"; exit 1; }; \
	for tok in --bg --panel --accent --ok --warn --err; do \
	  grep -q -- "$$tok" $$web/app.css || { echo "ui-lint: $$tok topilmadi"; exit 1; }; done; \
	grep -qiE 'https?://(cdn|unpkg|jsdelivr)' $$web/*.html $$web/*.js && { echo "ui-lint: tashqi CDN taqiqlangan"; exit 1; } || true; \
	python3 scripts/jscheck.py $$web/app.js || exit 1; \
	echo "ui-lint: OK ($$n ekran)"

## UI skrinshotlari (Chrome headless) — dizaynni ko'z bilan tekshirish uchun
ui-shot: build
	@./scripts/ui-shot.sh

## Integratsiya matritsasi (C1.14 dan boshlab to'la ishlaydi)
itest:
	./scripts/itest.sh $(SCENARIO)

## 2000 kamera stressi (C4.8)
stress:
	./scripts/stress.sh $(CAMS)

## Tar qadoq (joylarga ko'chirish uchun)
package: build
	tar -C $(BUILD_DIR) -czf $(BUILD_DIR)/$(BIN)-$(VERSION)-linux-$(GOARCH).tar.gz \
	  $(BIN) config systemd install.sh VERSION
	@echo "qadoq: $(BUILD_DIR)/$(BIN)-$(VERSION)-linux-$(GOARCH).tar.gz"

## Joyda SSH orqali yangilash: SSH_HOST=192.168.x.x make remote-update
remote-update: package
	@test -n "$$SSH_HOST" || { echo "SSH_HOST=... kerak"; exit 1; }
	scp $(BUILD_DIR)/$(BIN)-$(VERSION)-linux-$(GOARCH).tar.gz $$SSH_HOST:/tmp/
	ssh $$SSH_HOST 'sudo tar -xzf /tmp/$(BIN)-$(VERSION)-linux-$(GOARCH).tar.gz -C /opt/vmsagent && sudo systemctl restart $(BIN)'
	@echo "yangilandi: $$SSH_HOST"

install: build
	cd $(BUILD_DIR) && ./install.sh

uninstall:
	@sudo systemctl disable --now $(BIN) 2>/dev/null || true
	@sudo rm -f /etc/systemd/system/$(BIN).service
	@sudo systemctl daemon-reload
	@echo "unit o'chirildi; /opt/$(BIN) va /var/lib/$(BIN) qoldi (parollar saqlanadi)"

logs: ## CLIENT logi: fayl bo'lsa undan, bo'lmasa journald (ikkalasi bir xil mazmundа)
	@if [ -f /var/log/vmsagent/vmsagent.log ]; then tail -f /var/log/vmsagent/vmsagent.log; \
	else journalctl -u $(BIN) -f; fi

backup: ## §10.1/C4.5: agent holatini zaxiralash (agent.key+secrets.bin+state.json)
	bash scripts/agent-backup.sh $(BACKUP_DIR)

restore: ## zaxiradan tiklash: make restore ARCHIVE=vmsagent-state-*.tar.gz
	@test -n "$(ARCHIVE)" || { echo "make restore ARCHIVE=<fayl.tar.gz>"; exit 1; }
	bash scripts/agent-restore.sh "$(ARCHIVE)"

logs-err: ## faqat xatolar (level=ERROR) — fayldan yoki journald'dan
	@if [ -f /var/log/vmsagent/vmsagent.log ]; then grep --line-buffered "level=ERROR" /var/log/vmsagent/vmsagent.log | tail -f; \
	else journalctl -u $(BIN) -p err -f; fi

status:
	systemctl status $(BIN) --no-pager || true
	curl -fsS http://127.0.0.1:8082/healthz || true

## Ishga tushirilgan JARAYONLARNI to'xtatish (make start / dev / devserver).
## PID fayl + aniq pattern'lar. `go run` bolasi go-build kachida turgani uchun
## alohida pattern kerak (SIGTERM `go run`ga yetib, bolaga yetmaydi).
## Pattern'lardagi [x] trigi — pkill o'z shell'ini o'ldirmasin.
stop:
	@stopped=0; \
	for f in $(RUN_DIR)/*.pid; do \
	  [ -e "$$f" ] || continue; \
	  pid=$$(cat "$$f" 2>/dev/null || true); \
	  if [ -n "$$pid" ] && kill -0 "$$pid" 2>/dev/null; then \
	    echo "stop: SIGTERM -> pid $$pid  ($$f)"; kill -TERM "$$pid" 2>/dev/null || true; stopped=1; \
	  fi; \
	  rm -f "$$f"; \
	done; \
	for pat in $(STOP_PATTERNS); do \
	  if pkill -TERM -f "$$pat" 2>/dev/null; then echo "stop: SIGTERM -> pattern $$pat"; stopped=1; fi; \
	done; \
	sleep 1; \
	for pat in $(STOP_PATTERNS); do \
	  pkill -KILL -f "$$pat" 2>/dev/null || true; \
	done; \
	if [ "$$stopped" = "1" ]; then echo "stop: to'xtatildi"; else echo "stop: ishga tushirilgan jarayon yo'q"; fi; \
	if systemctl is-active --quiet $(BIN) 2>/dev/null; then \
	  echo "ESLATMA: systemd xizmati faol — uni 'sudo make stop-service' bilan to'xtating"; \
	fi

## systemd xizmatini to'xtatish (prod o'rnatilgan bo'lsa)
stop-service:
	@sudo systemctl stop $(BIN) && echo "stop-service: $(BIN) to'xtatildi"

clean:
	rm -rf $(BUILD_DIR) coverage.out

## GitHub'ga yuklash: BARCHA o'zgarishlarni default izoh bilan commit qilib push qiladi.
## Izohni almashtirish: make push MSG="fix(client): …"  (bo‘sh → avtomatik izoh)
## Eslatma: repositoriya ildoshi client/ ustida — server/ o‘zgarishlari ham yuboriladi.
push:
	@./scripts/git-push.sh "$(MSG)"

## GitHub'dan yangilash: origin'dan tortib papkani yangilaydi (autostash, faqat fast-forward).
update:
	@./scripts/git-update.sh

help:
	@grep -hE '^## ' Makefile | sed 's/^## /  /'
