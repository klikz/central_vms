# vmsd — build/ishga tushirish qoidalari (root REJA §4.2, server REJA §5).
# Medium effort rejimi: har qadamdan keyin `make fmt vet lint race` (server TASKS §0.4 qoidalari).

MODULE      := central-vms/server
BIN         := vmsd
BUILD_DIR   := build
# Barcha yig'ilgan artefaktlar VERSIYALANGAN holda `builds/<versiyada>/`
# ichida saqlanadi (2026-11-23 foydalanuvchi talabi). `build/` esa
# ish/reja staging'i bo'lib qoladi: eng yangi linux binar shu yerga
# nusxalanadi (start/run/seed/soak/systemd oqimlari uzilmasligi uchun).
BUILDS_DIR  := builds
RUN_DIR     := $(BUILD_DIR)/run
GO          := go
GOFLAGS     := -trimpath
CGO         := 0
GOOS        ?= linux
GOARCH      ?= amd64
# make buildWin WINARCH=arm64 bilan ham yig'iladi (DIQQAT: Make
# qator-uzi izohidan oldingi bo'shliqlarni qiymatga qo'shadi — izoh alohida qatorda).
WINARCH     ?= amd64

# MediaMTX versiyasi QULFLANGAN (server REJA §2.1/3 — tasdiqlangan 2026-11-20).
MEDIAMTX_VERSION := v1.11.3
MEDIAMTX_BIN     := mediamtx/mediamtx
MEDIAMTX_URL     := https://github.com/bluenviron/mediamtx/releases/download/$(MEDIAMTX_VERSION)/mediamtx_$(MEDIAMTX_VERSION)_linux_amd64.tar.gz

# Portlar (root §16.4, REJA §2.1/1): TCP + UDP tekshiriladi.
# TCP: vmsd API, MediaMTX RTSP/HLS/WebRTC/auth-hook(vmsd)/API/metrics/pprof.
# UDP: MediaMTX RTSP RTP/RTCP va WebRTC ICE (8189!) — tashqi MediaMTX ham shularni ushlaydi.
TCP_PORTS := 8081 8554 8888 8889 8189 9997 9998 9999
UDP_PORTS := 8000 8001 8189

VERSION   := $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT    := $(shell git rev-parse --short HEAD 2>/dev/null || echo none)
BUILD_AT  := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS   := -s -w -X $(MODULE)/internal/version.Version=$(VERSION) \
             -X $(MODULE)/internal/version.Commit=$(COMMIT) \
             -X $(MODULE)/internal/version.BuiltAt=$(BUILD_AT)

COVER_ALL := 50

STOP_PATTERNS := 'build/[v]msd -config' 'exe/[v]msd' 'go-build[^ ]*/[v]msd' \
                 'mediamtx/[m]ediamtx mediamtx/mediamtx.yml'

.PHONY: all start dev build run media media-get probe migrate-up migrate-down migrate-status admin \
        ui-build ui-dev ui-test test race cover gate lint fmt vet package install logs logs-file stop \
        stop-service doctor metrics backup-now soak buildWin clean help

all: build

## Port tekshiruvi: band bo'lsa aniq xato (tashqi xizmatni O'ZI to'xtatmaydi).
port-check:
	@bad=""; \
	for p in $(TCP_PORTS); do \
	  if ss -ltn 2>/dev/null | awk '{print $$4}' | grep -Eq ":$$p$$"; then bad="$$bad tcp/$$p"; fi; \
	done; \
	for p in $(UDP_PORTS); do \
	  if ss -lun 2>/dev/null | awk '{print $$4}' | grep -Eq ":$$p$$"; then bad="$$bad udp/$$p"; fi; \
	done; \
	if [ -n "$$bad" ]; then \
	  echo "XATO: band portlar:$$bad" >&2; \
	  echo "Yechim: tashqi MediaMTX'ni to'xtating — masalan: sudo systemctl stop premier-cv-mediamtx" >&2; \
	  exit 1; \
	fi

## Hammasini quradi → migratsiya → MediaMTX + vmsd (dev). Ctrl+C ikkalasini ham to'xtatadi.
start: build port-check
	@mkdir -p $(RUN_DIR) var/log
	@./$(BUILD_DIR)/$(BIN) -config config/dev.yaml -migrate up
	$(MEDIAMTX_BIN) mediamtx/mediamtx.yml & echo $$! > $(RUN_DIR)/mediamtx.pid; \
	./$(BUILD_DIR)/$(BIN) -config config/dev.yaml & echo $$! > $(RUN_DIR)/$(BIN).pid; \
	trap 'kill $$(cat $(RUN_DIR)/mediamtx.pid) $$(cat $(RUN_DIR)/$(BIN).pid) 2>/dev/null' INT TERM EXIT; \
	wait $$(cat $(RUN_DIR)/$(BIN).pid)

## Tez ish: backend go run (hot reload qo'lda); MediaMTX alohida (`make media`).
dev:
	@mkdir -p $(RUN_DIR) var/log
	$(GO) run ./cmd/$(BIN) -config config/dev.yaml & echo $$! > $(RUN_DIR)/dev-$(BIN).pid; \
	wait $$(cat $(RUN_DIR)/dev-$(BIN).pid)

## Artefaktlar: builds/<versiyada>/<bin>-<os>-<arch>; build/ staging (root §4.1:
## vmsd, web/, migrations/, mediamtx.yml, config, VERSION, run.sh).
build:
	@mkdir -p $(BUILD_DIR)/web $(BUILD_DIR)/migrations $(BUILD_DIR)/config $(BUILD_DIR)/mediamtx $(BUILDS_DIR)/$(VERSION)
	CGO_ENABLED=$(CGO) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILDS_DIR)/$(VERSION)/$(BIN)-$(GOOS)-$(GOARCH) ./cmd/$(BIN)
	CGO_ENABLED=$(CGO) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -o $(BUILDS_DIR)/$(VERSION)/testagent-$(GOOS)-$(GOARCH) ./cmd/testagent
	cp -f $(BUILDS_DIR)/$(VERSION)/$(BIN)-$(GOOS)-$(GOARCH) $(BUILD_DIR)/$(BIN)
	cp -f $(BUILDS_DIR)/$(VERSION)/testagent-$(GOOS)-$(GOARCH) $(BUILD_DIR)/testagent
	cp -f migrations/*.sql $(BUILD_DIR)/migrations/
	cp -f config/prod.yaml.example $(BUILD_DIR)/config/config.yaml
	cp -f mediamtx/mediamtx.yml $(BUILD_DIR)/mediamtx/mediamtx.yml
	cp -f run.sh $(BUILD_DIR)/run.sh
	printf '%s (commit %s, %s)\n' "$(VERSION)" "$(COMMIT)" "$(BUILD_AT)" > $(BUILD_DIR)/VERSION
	@$(MAKE) --no-print-directory ui-build
	@echo "builds/$(VERSION)/ tayyor: $$(ls $(BUILDS_DIR)/$(VERSION))"

## Windows uchun cross-compile (CGO=0 — modernc.org/sqlite sof Go, shuning
## uchun mingw TALAB QILINMAYDI). Artefakt: builds/<versiyada>/*.exe
##   make buildWin            # amd64 (standapt)
##   make buildWin WINARCH=arm64
buildWin:
	@mkdir -p $(BUILDS_DIR)/$(VERSION)
	CGO_ENABLED=0 GOOS=windows GOARCH=$(WINARCH) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILDS_DIR)/$(VERSION)/$(BIN)-windows-$(WINARCH).exe ./cmd/$(BIN)
	CGO_ENABLED=0 GOOS=windows GOARCH=$(WINARCH) $(GO) build $(GOFLAGS) -o $(BUILDS_DIR)/$(VERSION)/testagent-windows-$(WINARCH).exe ./cmd/testagent
	@echo "tayyor: $(BUILDS_DIR)/$(VERSION)/$(BIN)-windows-$(WINARCH).exe"

## Faqat build/vmsd ni build/config bilan ishga tushiradi.
run:
	@mkdir -p $(RUN_DIR)
	./$(BUILD_DIR)/$(BIN) -config $(BUILD_DIR)/config/config.yaml & echo $$! > $(RUN_DIR)/$(BIN).pid; \
	wait $$(cat $(RUN_DIR)/$(BIN).pid)

## MediaMTX ni loyiha config'i bilan ishga tushiradi (make start buni o'zi qiladi).
media: port-check
	$(MEDIAMTX_BIN) mediamtx/mediamtx.yml

## Qulflangan versiyadagi MediaMTX binary'sini yuklaydi (git'da saqlanmaydi).
media-get:
	@if [ -x $(MEDIAMTX_BIN) ] && [ "$$($(MEDIAMTX_BIN) --version 2>/dev/null)" = "$(MEDIAMTX_VERSION)" ]; then \
	  echo "MediaMTX $(MEDIAMTX_VERSION) allaqachon mavjud: $(MEDIAMTX_BIN)"; \
	else \
	  echo "MediaMTX $(MEDIAMTX_VERSION) yuklanmoqda..."; \
	  curl -fsSL -o /tmp/mediamtx.tgz $(MEDIAMTX_URL) && \
	  tar -xzf /tmp/mediamtx.tgz -C mediamtx mediamtx && rm /tmp/mediamtx.tgz && \
	  chmod +x $(MEDIAMTX_BIN) && echo "o'rnatildi: $$($(MEDIAMTX_BIN) --version)"; \
	fi

## Server quvvati: backend aniqlash (S2'da parallel limit O'LCHANADI — REJA §2.1/5).
probe: build
	./$(BUILD_DIR)/$(BIN) -config config/dev.yaml -probe

migrate-up:
	./$(BUILD_DIR)/$(BIN) -config config/dev.yaml -migrate up

migrate-down:
	./$(BUILD_DIR)/$(BIN) -config config/dev.yaml -migrate down

migrate-status:
	./$(BUILD_DIR)/$(BIN) -config config/dev.yaml -migrate status

## S2 qabul mezoni: real MediaMTX + real FFmpeg bilan uchdan-uchga tekshiruv
e2e-s2:
	./scripts/e2e_s2.sh

e2e-s3:
	./scripts/e2e_s3.sh

## O'zini-tekshirish: config/DB/MediaMTX/portlar/zaxira/disk (2026-11-22).
## Server ishlab turganda ham ishlaydi; FAIL bo'lsa exit 1.
doctor: build
	./$(BUILD_DIR)/$(BIN) -config $${CFG:-config/dev.yaml} -doctor

## Ichki metrikalar (127.0.0.1:8189/metrics). METRICS_URL=... bilan
## boshqa manzil ham mumkin.
metrics:
	@curl -fsS "$${METRICS_URL:-http://127.0.0.1:8189/metrics}"

## Zaxirani taymer kutmasdan hozir qilish (test/tiklash mashqi uchun).
backup-now: build
	@VMSD_BIN=$(CURDIR)/$(BUILD_DIR)/$(BIN) VMSD_CONFIG=$${CFG:-$(CURDIR)/config/dev.yaml} \
	  bash deploy/backup.sh "$${DB_PATH:-var/vmsd.db}" "$${BACKUP_DIR:-var/backups}" 14

## Soak: uzoq ishlash + uzilish in'ektsiyasi (scripts/soak.sh).
## HOURS=0 → ~10 daqiqalik tutun sinovi; HOURS=24 → tunlik.
soak:
	@HOURS=$${HOURS:-0} bash scripts/soak.sh

## Birinchi admin: make admin USER=admin (parol so'raladi, terminalga chiqmaydi)
## Admin yaratish/yangilash (login mavjud bo'lsa parolini yangilaydi).
## Parol ekranda ko'rinmaydi; DB'da argon2id hash saqlanadi.
admin: build
	@read -rp "Login [admin]: " U; U=$${U:-admin}; \
	read -rp "Parol (kamida 8 belgi, ko'rinmaydi): " -s P; echo; \
	read -rp "Parol (takror): " -s P2; echo; \
	if [ "$$P" != "$$P2" ]; then echo "XATO: parollar mos kelmadi" >&2; exit 1; fi; \
	./$(BUILD_DIR)/$(BIN) -config config/dev.yaml -seed-admin "$$U:$$P"

## UI build (React+Vite, webui/). Node PATH'da bo'lmasa ~/.local/node/bin
## va nvm versiyalari sinab ko'riladi (dev mashina talabi — prod'ga bog'liq emas).
NODE_DIR ?= $(firstword $(wildcard $(HOME)/.local/node/bin) $(wildcard $(HOME)/.nvm/versions/node/*/bin))

ui-build:
	@mkdir -p $(BUILD_DIR)/web; \
	export PATH="$(NODE_DIR):$$PATH"; \
	if [ ! -f webui/package.json ] || ! command -v npm >/dev/null 2>&1; then \
	  echo "UI build qilinmadi: npm topilmadi (node PATH'da yo'q — NODE_DIR=... belgilang)"; \
	  echo "<html><body>VMS UI hali yig'ilgan emas.</body></html>" > $(BUILD_DIR)/web/index.html; \
	  exit 0; \
	fi; \
	(cd webui && npm ci --no-audit --no-fund >/dev/null && npm run build >/dev/null) && \
	rm -rf $(BUILD_DIR)/web && cp -r webui/dist $(BUILD_DIR)/web && \
	echo "UI yig'ildi: $(BUILD_DIR)/web ($$(du -sh $(BUILD_DIR)/web | cut -f1))"

ui-test:
	@export PATH="$(NODE_DIR):$$PATH"; cd webui && npx vitest run

ui-dev:
	@export PATH="$(NODE_DIR):$$PATH"; \
	if ! command -v npm >/dev/null 2>&1; then echo "npm topilmadi (NODE_DIR=...)"; exit 1; fi; \
	cd webui && npm run dev

## Testlar (unit, race yo'q — tez)
test:
	$(GO) test ./... -count=1

## Race detector + coverage (majburiy tekshiruv)
race:
	$(GO) test ./... -race -count=1 -coverprofile=coverage.out -covermode=atomic

cover: race
	$(GO) tool cover -func=coverage.out | tail -1

## Gate: fmt+vet+lint+race + coverage bo'sagasi (TASKS §0.4/§0.6)
gate: fmt vet lint race
	@tot=$$($(GO) tool cover -func=coverage.out | awk '/^total:/ {gsub(/%/,"",$$3); print $$3}'); \
	echo "coverage: $$tot% (bo'saga $(COVER_ALL)%)"; \
	awk -v t="$$tot" -v m=$(COVER_ALL) 'BEGIN { exit (t+0 < m+0) ? 1 : 0 }'

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

## build/ ni tar.gz qilib qadoqlaydi (serverga ko'chirish uchun).
package: build
	@mkdir -p $(BUILDS_DIR)/$(VERSION)
	tar -czf $(BUILDS_DIR)/$(VERSION)/vmsd-$(VERSION)-linux-$(GOARCH).tar.gz -C $(BUILD_DIR) .
	@echo "paket: $(BUILDS_DIR)/$(VERSION)/vmsd-$(VERSION)-linux-$(GOARCH).tar.gz"

## Prod o'rnatish: /opt/vmsd + systemd unitlar (root §4.2; S4'da to'liq tekshiriladi).
install: package
	@test $$(id -u) -eq 0 || { echo "root kerak: sudo make install" >&2; exit 1; }
	mkdir -p /opt/vmsd /var/lib/vmsd /var/log/vmsd /var/backups/vmsd
	cp -r $(BUILD_DIR)/vmsd $(BUILD_DIR)/web $(BUILD_DIR)/migrations $(BUILD_DIR)/mediamtx /opt/vmsd/
	[ -f /opt/vmsd/config/config.yaml ] || cp config/prod.yaml.example /opt/vmsd/config/config.yaml
	install -m 755 deploy/backup.sh /opt/vmsd/backup.sh
	install -m 755 scripts/alert.sh /opt/vmsd/alert.sh
	cp -f systemd/vmsd.service systemd/vms-media.service systemd/vmsd-backup.service systemd/vmsd-backup.timer systemd/vms-alert.service systemd/vms-alert.timer /etc/systemd/system/
	systemctl daemon-reload && systemctl enable --now vms-media vmsd vmsd-backup.timer vms-alert.timer
	@echo "o'rnatildi. PROBE MAJBURIY (GPU o'zgaruvchan): sudo -u vmsd /opt/vmsd/vmsd -config /opt/vmsd/config/config.yaml -probe"

## Journal (prod) yoki dev log fayli (alohida — client logi bilan aralashmaydi).
logs:
	@if systemctl is-active --quiet vmsd 2>/dev/null; then journalctl -u vmsd -f; else tail -F var/log/vmsd.log; fi

logs-file:
	@tail -n 200 var/log/vmsd.log

## make start / dev ko'targan jarayonlarni mayin to'xtatish.
stop:
	@for p in $(STOP_PATTERNS); do pkill -TERM -f "$$p" 2>/dev/null || true; done
	@rm -f $(RUN_DIR)/*.pid

stop-service:
	sudo systemctl stop vmsd vms-media

clean:
	rm -rf $(BUILD_DIR) $(BUILDS_DIR) coverage.out

## GitHub'ga yuklash / yangilash (skriptlar client/scripts/ da — bitta repoda).
push:
	@../client/scripts/git-push.sh "$(MSG)"

update:
	@../client/scripts/git-update.sh

help:
	@grep -B1 '^## ' Makefile | grep -v '^--$$' | sed 's/^## //'
