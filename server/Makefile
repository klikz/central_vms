# vmsd — build/ishga tushirish qoidalari (root REJA §4.2, server REJA §5).
# Medium effort rejimi: har qadamdan keyin `make fmt vet lint race` (server TASKS §0.4 qoidalari).

MODULE      := central-vms/server
BIN         := vmsd
BUILD_DIR   := build
RUN_DIR     := $(BUILD_DIR)/run
GO          := go
GOFLAGS     := -trimpath
CGO         := 0
GOOS        ?= linux
GOARCH      ?= amd64

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

.PHONY: all start dev build run media media-get probe migrate-up migrate-down migrate-status \
        ui-build ui-dev test race cover gate lint fmt vet package install logs logs-file stop \
        stop-service clean help

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

## build/ tarkibi root §4.1: vmsd, web/, migrations/, mediamtx.yml, config, VERSION, run.sh
build:
	@mkdir -p $(BUILD_DIR)/web $(BUILD_DIR)/migrations $(BUILD_DIR)/config $(BUILD_DIR)/mediamtx
	CGO_ENABLED=$(CGO) GOOS=$(GOOS) GOARCH=$(GOARCH) $(GO) build $(GOFLAGS) -ldflags "$(LDFLAGS)" -o $(BUILD_DIR)/$(BIN) ./cmd/$(BIN)
	cp -f migrations/*.sql $(BUILD_DIR)/migrations/
	cp -f config/prod.yaml.example $(BUILD_DIR)/config/config.yaml
	cp -f mediamtx/mediamtx.yml $(BUILD_DIR)/mediamtx/mediamtx.yml
	cp -f run.sh $(BUILD_DIR)/run.sh
	printf '%s (commit %s, %s)\n' "$(VERSION)" "$(COMMIT)" "$(BUILD_AT)" > $(BUILD_DIR)/VERSION
	@$(MAKE) --no-print-directory ui-build
	@echo "build/ tayyor: $$(ls $(BUILD_DIR))"

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

## UI build (S3'da React+Vite; hozir npm bo'lmasa placeholder — REJA §3 embed YO'Q).
ui-build:
	@if [ -f webui/package.json ] && command -v npm >/dev/null 2>&1; then \
	  (cd webui && npm ci && npm run build) && \
	  rm -rf $(BUILD_DIR)/web && cp -r webui/dist $(BUILD_DIR)/web; \
	else \
	  echo "UI S3 bosqichida (React+Vite). Hozirchi holat: API /healthz ishlayapti." > $(BUILD_DIR)/web/index.html; \
	fi

ui-dev:
	@if command -v npm >/dev/null 2>&1 && [ -f webui/package.json ]; then \
	  (cd webui && npm run dev -- --proxy 8081); \
	else \
	  echo "npm/webui topilmadi — UI S3'da keladi"; \
	fi

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
	tar -czf $(BUILD_DIR)/vmsd-$(VERSION)-linux-$(GOARCH).tar.gz -C $(BUILD_DIR) .
	@echo "paket: $(BUILD_DIR)/vmsd-$(VERSION)-linux-$(GOARCH).tar.gz"

## Prod o'rnatish: /opt/vmsd + systemd unitlar (root §4.2; S4'da to'liq tekshiriladi).
install: package
	@test $$(id -u) -eq 0 || { echo "root kerak: sudo make install" >&2; exit 1; }
	mkdir -p /opt/vmsd /var/lib/vmsd /var/log/vmsd
	cp -r $(BUILD_DIR)/vmsd $(BUILD_DIR)/web $(BUILD_DIR)/migrations $(BUILD_DIR)/mediamtx /opt/vmsd/
	[ -f /opt/vmsd/config/config.yaml ] || cp config/prod.yaml.example /opt/vmsd/config/config.yaml
	cp -f systemd/vmsd.service systemd/vms-media.service /etc/systemd/system/
	systemctl daemon-reload && systemctl enable --now vms-media vmsd
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
	rm -rf $(BUILD_DIR) coverage.out

help:
	@grep -B1 '^## ' Makefile | grep -v '^--$$' | sed 's/^## //'
