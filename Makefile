# Central VMS — ILDOV Makefile: server (`vmsd`) VA agent (`vmsagent`) BITTA
# mashinada ko'tarib, kamerani uchidan tekshirish uchun (2026-11-20).
#
#   make build     — server UI + vmsd + vmsagent
#   make run       — MediaMTX + vmsd + vmsagent (localhost konfiguratsiyasi)
#   make addcam    — agentga kamera qo'shish (parol: secrets/camera.env)
#   make check     — to'liq zanjir: kamera → holat → preview → kadr
#   make stop      — uchalasini to'xtatish
#   make gate      — ikkala tomondagi test darvozalari

NODE_DIR ?= $(firstword $(wildcard $(HOME)/.local/node/bin) $(wildcard $(HOME)/.nvm/versions/node/*/bin))
export PATH := $(NODE_DIR):$(PATH)

SRV    := server
CLI    := client
RUN    := $(SRV)/build/run
LOG    := $(SRV)/var/log
BIN    := $(SRV)/build/vmsd
AGENT  := $(CLI)/build/vmsagent
MTX    := $(SRV)/mediamtx/mediamtx
SCFG   := config/localhost.yaml
CCFG   := config/client-localhost.yaml
SRV_URL := http://127.0.0.1:8081
UI_URL  := http://127.0.0.1:8082

ADMIN      ?= admin
ADMIN_PASS ?= Dev-Salom-2026
CAM_NAME   ?= Hik-24
CAM_HOST   ?= 192.168.0.24
CAM_PORT   ?= 554
CAM_USER   ?= admin
CAM_MAIN   ?= /Streaming/Channels/101
CAM_SUB    ?= /Streaming/Channels/102
SECRETS    := secrets/camera.env

PY := python3

.PHONY: all build media server agent run stop restart status token seed join approve cams addcam delcam preview frame check gate server-gate client-gate ui-test logs push update

all: build

build:
	$(MAKE) -C $(SRV) ui-build build
	$(MAKE) -C $(CLI) build

## --- Ishga tushirish (bitta mashina) ---

media:
	@mkdir -p $(RUN) $(LOG)
	@if [ -f $(RUN)/mediamtx.pid ] && kill -0 $$(cat $(RUN)/mediamtx.pid) 2>/dev/null; then \
		echo "MediaMTX allaqachon ishlayapti ($$(cat $(RUN)/mediamtx.pid))"; \
	else \
		( cd $(SRV) && setsid ./mediamtx/mediamtx mediamtx/mediamtx.yml > ../$(LOG)/mtx-localhost.log 2>&1 & echo $$! > $(CURDIR)/$(RUN)/mediamtx.pid ); \
		sleep 3; echo "MediaMTX ishga tushdi"; \
	fi

seed: build
	@cd $(SRV) && ./build/vmsd -config $(SCFG) -migrate up >/dev/null 2>&1; \
	./build/vmsd -config $(SCFG) -seed-admin "$(ADMIN):$(ADMIN_PASS)" >/dev/null 2>&1 || true; \
	echo "admin foydalanuvchi tayyor: $(ADMIN)"

server: build seed
	@mkdir -p $(RUN) $(LOG)
	@if [ -f $(RUN)/vmsd-localhost.pid ] && kill -0 $$(cat $(RUN)/vmsd-localhost.pid) 2>/dev/null; then \
		echo "vmsd allaqachon ishlayapti ($$(cat $(RUN)/vmsd-localhost.pid))"; \
	else \
		( cd $(SRV) && setsid ./build/vmsd -config $(SCFG) > ../$(LOG)/vmsd-localhost.out 2>&1 & echo $$! > $(CURDIR)/$(RUN)/vmsd-localhost.pid ); \
		for i in 1 2 3 4 5 6 7 8; do curl -sf $(SRV_URL)/healthz >/dev/null 2>&1 && break; sleep 1; done; \
		curl -sf $(SRV_URL)/healthz >/dev/null && echo "vmsd: $(SRV_URL)" || { echo "vmsd ko'tarilmadi — $(LOG)/vmsd-localhost.out"; exit 1; }; \
	fi

agent: build
	@mkdir -p $(RUN) $(CLI)/var/agent-state $(CLI)/var/log
	@if [ -f $(RUN)/vmsagent.pid ] && kill -0 $$(cat $(RUN)/vmsagent.pid) 2>/dev/null; then \
		echo "vmsagent allaqachon ishlayapti ($$(cat $(RUN)/vmsagent.pid))"; \
	else \
		( cd $(CLI) && setsid ./build/vmsagent -config $(CCFG) > var/log/vmsagent.out 2>&1 & echo $$! > $(CURDIR)/$(RUN)/vmsagent.pid ); \
		sleep 3; echo "vmsagent: $(UI_URL)"; \
	fi

run: media server agent
	@sleep 8; $(MAKE) --no-print-directory join; sleep 6; $(MAKE) --no-print-directory approve; sleep 10; $(MAKE) --no-print-directory status

stop:
	@for f in $(RUN)/vmsagent.pid $(RUN)/vmsd-localhost.pid $(RUN)/mediamtx.pid; do \
		[ -f $$f ] && kill $$(cat $$f) 2>/dev/null && echo "to'xtatildi: $$f"; rm -f $$f; \
	done

restart: stop; sleep 2; $(MAKE) run

status:
	@echo "== agent (lokal UI):"; \
	curl -s $(UI_URL)/api/ui/status | $(PY) -c 'import sys,json;d=json.load(sys.stdin);print("   agent_id:",d.get("agent_id"),"| server online:",d.get("server_online"),"| kameralar:",d.get("cams_online"),"/",d.get("cams_total"))' 2>/dev/null || echo "   agent javob bermadi"; \
	echo "== server mijozlari:"; \
	TOK=$$(curl -s -X POST $(SRV_URL)/api/auth/login -d '{"username":"$(ADMIN)","password":"$(ADMIN_PASS)"}' | $(PY) -c 'import sys,json;print(json.load(sys.stdin).get("access",""))'); \
	curl -s $(SRV_URL)/api/clients -H "Authorization: Bearer $$TOK" | $(PY) -c 'import sys,json;[print("   ",c["id"],c["name"],c["status"],c["online_count"],"/",c["cameras_count"]) for c in json.load(sys.stdin)]' 2>/dev/null || echo "   server javob bermadi"

token:
	@curl -s -X POST $(SRV_URL)/api/auth/login -d '{"username":"$(ADMIN)","password":"$(ADMIN_PASS)"}' | $(PY) -c 'import sys,json;print(json.load(sys.stdin).get("access",""))'

join:
	@curl -s -X POST $(UI_URL)/api/ui/join -H 'Content-Type: application/json' -d '{"name":"$(CAM_NAME)-joy"}' | head -c 160; echo

approve:
	@TOK=$$(curl -s -X POST $(SRV_URL)/api/auth/login -d '{"username":"$(ADMIN)","password":"$(ADMIN_PASS)"}' | $(PY) -c 'import sys,json;print(json.load(sys.stdin).get("access",""))'); \
	RID=$$(curl -s "$(SRV_URL)/api/admin/joins?status=pending" -H "Authorization: Bearer $$TOK" | $(PY) -c 'import sys,json;d=json.load(sys.stdin);r=d.get("requests",[]);print(r[0]["id"] if r else "")'); \
	if [ -n "$$RID" ]; then \
		curl -s -X POST "$(SRV_URL)/api/admin/joins/$$RID/approve" -H "Authorization: Bearer $$TOK" -d '{"name":"Lokal-sinov"}' | head -c 160; echo; \
	else echo "tasdiq kutayotgan so'rov yo'q"; fi

## --- Kameralar ---

addcam:
	@[ -f $(SECRETS) ] && . ./$(SECRETS); \
	if [ -z "$$CAM_PASS" ]; then echo "XATO: $(SECRETS) ichida CAM_PASS bo'lishi kerak (git'ga kirmaydi)"; exit 1; fi; \
	curl -s -X POST $(UI_URL)/api/ui/cams -H 'Content-Type: application/json' \
		-d "{\"name\":\"$(CAM_NAME)\",\"host\":\"$(CAM_HOST)\",\"port\":$(CAM_PORT),\"main_path\":\"$(CAM_MAIN)\",\"sub_path\":\"$(CAM_SUB)\",\"secret\":\"$$CAM_PASS\",\"enabled\":true}" | head -c 200; echo

delcam:
	@CID=$$(curl -s $(UI_URL)/api/ui/cams | $(PY) -c 'import sys,json;d=json.load(sys.stdin);c=d.get("cams",d) if isinstance(d,dict) else d;print(next((x["id"] for x in c if x.get("name")=="$(CAM_NAME)"),""))'); \
	if [ -n "$$CID" ]; then curl -s -X DELETE "$(UI_URL)/api/ui/cams/$$CID" | head -c 120; echo; else echo "$(CAM_NAME) topilmadi"; fi

cams:
	@TOK=$$(curl -s -X POST $(SRV_URL)/api/auth/login -d '{"username":"$(ADMIN)","password":"$(ADMIN_PASS)"}' | $(PY) -c 'import sys,json;print(json.load(sys.stdin).get("access",""))'); \
	for CID in $$(curl -s $(SRV_URL)/api/clients -H "Authorization: Bearer $$TOK" | $(PY) -c 'import sys,json;print(" ".join(str(c["id"]) for c in json.load(sys.stdin)))'); do \
	  echo "   client $$CID:"; \
	  curl -s "$(SRV_URL)/api/clients/$$CID/cameras" -H "Authorization: Bearer $$TOK" | $(PY) -c 'import sys,json;[print("     ",c["name"],"|",c["rtsp_masked"],"|",c["status"],"| check:",c.get("last_check_at") or "-","| err:",c.get("last_error") or "-","| codec:",c.get("codec_main")) for c in json.load(sys.stdin)]'; \
	done

preview:
	@TOK=$$(curl -s -X POST $(SRV_URL)/api/auth/login -d '{"username":"$(ADMIN)","password":"$(ADMIN_PASS)"}' | $(PY) -c 'import sys,json;print(json.load(sys.stdin).get("access",""))'); \
	CAM=$$(curl -s $(UI_URL)/api/ui/cams | $(PY) -c 'import sys,json;d=json.load(sys.stdin);c=d.get("cams",d) if isinstance(d,dict) else d;print(next((x["id"] for x in c if x.get("name")=="$(CAM_NAME)"),""))'); \
	curl -s -X POST "$(SRV_URL)/api/cameras/$$CAM/preview" -H "Authorization: Bearer $$TOK" -H 'Content-Type: application/json' -d '{"profile":"sub"}' > $(RUN)/preview.json; \
	$(PY) -c 'import json;d=json.load(open("$(RUN)/preview.json"));print("   whep:",d.get("whep_url"));print("   rtsp:",d.get("rtsp_url"));print("   xato:",d.get("error")) if d.get("error") else None'

# frame — preview'ni QAYTA so'raydi (eski preview.json'dagi URL
# transcode yo'li o'zgarganda eskirgan bo'ladi: token 30 s, yo'l tc/...).
frame: preview
	@$(PY) -c 'import json;d=json.load(open("$(RUN)/preview.json"));open("$(RUN)/frame_url.txt","w").write(d["rtsp_url"]+"?token="+d["token"])'; \
	ffmpeg -hide_banner -loglevel error -rtsp_transport tcp -i "$$(cat $(RUN)/frame_url.txt)" -frames:v 1 -y $(RUN)/preview.jpg \
		&& echo "   KADR OLINDI: $(RUN)/preview.jpg ($$(stat -c%s $(RUN)/preview.jpg) bayt)" || echo "   KADR OLINMADI"

check:
	@echo "== 1. holat"; $(MAKE) --no-print-directory status
	@echo "== 2. kamera qo'shamiz"; $(MAKE) --no-print-directory addcam
	@echo "== 3. serverga yetib bordimi (30 s)"; sleep 30; $(MAKE) --no-print-directory cams
	@echo "== 4. preview"; $(MAKE) --no-print-directory preview
	@sleep 12; echo "== 5. kadr"; $(MAKE) --no-print-directory frame
	@echo "== 6. kamerani o'chiramiz"; $(MAKE) --no-print-directory delcam
	@sleep 10; echo "== 7. serverda qoldimi?"; $(MAKE) --no-print-directory cams

## --- Test darvozalari ---

server-gate:
	$(MAKE) -C $(SRV) gate

client-gate:
	$(MAKE) -C $(CLI) gate

ui-test:
	$(MAKE) -C $(SRV) ui-test

gate: server-gate client-gate ui-test

logs:
	@echo "--- vmsd:"; tail -5 $(LOG)/vmsd-localhost.log 2>/dev/null | tail -5
	@echo "--- agent:"; tail -5 $(CLI)/var/log/vmsagent.log 2>/dev/null

push:
	git add -A && read -rp "commit xabari: " M && git commit -m "$$M" && git push origin main

update:
	git pull --ff-only origin main
