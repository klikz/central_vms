# Agent holatini zaxiralash

> **Tayyor skriptlar (C4.5):** `sudo make backup` va
> `sudo make restore ARCHIVE=<fayl.tar.gz>` — quyidagi qo'llanma bilan bir xil
> ishlarni avtomat bajaradi (xizmatni to'xtatadi, 3 faylni oladi, tiklashda
> `*.pre-restore-<TS>` zaxira qoldiradi, egalik/ruhsatni tiklaydi). va tiklash (C2.20, docs/protocol.md §10.1)

Agentning **barcha holati** uchta faylda saqlanadi. Ular **uchala birga**
olinganida qayta tiklanadi — bittasi yo'q bo'lsa, qolganlari behuda (parollar
shifrlangan va kalitsiz ochilmaydi; `state.json` esa faqat `secret_ref`
ko'rsatkichlari, matn emas).

## Fayllar

| Fayl | Joyi | Nima saqlaydi | Ruxsat |
|------|------|---------------|--------|
| `agent.key` | `/etc/vmsagent/agent.key` | 32 bait tasodifiy AES kaliti | `0600` |
| `secrets.bin` | `/var/lib/vmsagent/secrets.bin` | AES-256-GCM parollar + `agent_secret` (`gcm1:<b64>` format) | `0600` |
| `state.json` | `/var/lib/vmsagent/state.json` | kameralar ro'yxati, `id` lar, `cams_epoch`, `secret_ref` lar (parol **yo'q**) | `0600` |

> `state.json` da parol yoki shifrlangan matn **umumiy holda** saqlanmaydi —
> faqat `secret_ref: "cam:<uuid>:main"` va `agent_secret_enc` kabi ref'lar.
> Shuning uchun `agent.key` + `secrets.bin` bo'lmasa parollar tiklanmaydi.

Yordamchi fayllar (zaxiraga **shart emas**, lekin shu bilan oling):
`state.json.bak` (avtomatik nusxa), `*.tmp` (atomik yozish qoldig'i — olingmasin).

## Zaxiralash

Agent **to'xtatilgan** holatda yoki kamida yozish bo'lmagan payt oling
(atomik yozish tufayli `state.json` hech qachon yarim holatda bo'lmaydi,
lekin `secrets.bin` + `state.json` bir lahzalik juftlik — birga oling).

```bash
sudo systemctl stop vmsagent

TS=$(date +%Y%m%d-%H%M%S)
sudo tar -czf "vmsagent-state-$TS.tar.gz" \
    -C / etc/vmsagent/agent.key \
    -C / var/lib/vmsagent/secrets.bin \
    -C / var/lib/vmsagent/state.json

sudo systemctl start vmsagent
```

Tekshirish (arxiv tarkibi):

```bash
tar -tzf "vmsagent-state-$TS.tar.gz"
# etc/vmsagent/agent.key
# var/lib/vmsagent/secrets.bin
# var/lib/vmsagent/state.json
```

**Xavfsizlik:** arxivda `agent.key` ochiq holda bor. Uni shifrlab saqlang va
faqat ishonchli joyda (masalan `gpg -c`):

```bash
gpg -c "vmsagent-state-$TS.tar.gz"   # vmsagent-state-$TS.tar.gz.gz chiqadi
```

Arxivni git'ga, serverga yoki ochiq papkaga **qo'ymang**. `agent.key` va
`secrets.bin` `.gitignore`'da allaqachon.

## Tiklash

Maqsad: bitta joyning (clientning) kaliti + parollari + kamera `id` lari
**o'zgarmasdan** qaytishi. `id` lar saqlanganligi uchun serverdagi
layout/favorites buzilmaydi (§6).

```bash
# 1) Agentni to'xtating
sudo systemctl stop vmsagent

# 2) Arxivni oching (avval gpg -d kerak bo'lsa)
gpg -d vmsagent-state-$TS.tar.gz | sudo tar -xz -C /      # yoki: sudo tar -xzf ... -C /

# 3) Ruxsatlarni tiklang (tar 0600 ni saqlagan, lekin tekshiring)
sudo chmod 0600 /etc/vmsagent/agent.key /var/lib/vmsagent/secrets.bin /var/lib/vmsagent/state.json

# 4) Kalit papkalari mavjudligi
sudo mkdir -p /etc/vmsagent /var/lib/vmsagent   # (arxiv ochilganda avtomatik)

# 5) Ishga tushiring
sudo systemctl start vmsagent
journalctl -u vmsagent -f
```

## Tiklashdan keyin nima kutiladi (to'g'rilash belgilari)

1. Agent **qayta enroll'siz** ulanadi — `state.json` da `agent_id` va
   `agent_secret_enc` bor, serverga `auth` shu bilan o'tadi (§3).
2. `journalctl` da `welcome qabul qilindi`, `cams_epoch` oldingi qiymatda.
3. Lokal UI'da kameralar ro'yxati to'liq, `id` lar o'zgarmagan.
4. Parollar ishlaydi (RTSP probe `online`) — demak `agent.key` + `secrets.bin`
   juftligi to'g'ri.
5. Agar kamera ro'yxati serverda eski bo'lib qolgan bo'lsa, `cams_epoch`
   farqi tufayli agent avtomatik `cams_sync` yuboradi (§6.5) — bu normal.

## Kalit yo'qolsa / buzilsa

`agent.key` yo'qolsa `secrets.bin` **qayta ochilmaydi** (AES-256-GCM).
Parollar yo'qoladi → kameralarni qayta kiriting (yoki CSV import), lekin
`id` lar `state.json` da qolgani uchun serverdagi layout saqlanadi.
Agar `state.json` ham yo'qolsa — kamera `id` lari yangidan yaratiladi va
serverda layout/favorites qayta sozlanadi. Shuning uchun **uchala fayl birga**
zaxira muhim.

## Sinov (lokal papkada, §10.1 tekshiruvi)

Haqiqiy tizimga tegmasdan, vaqtinchalik papkada zaxira→tiklash oqimini
tekshirish:

```bash
D=$(mktemp -d); mkdir -p "$D/etc" "$D/var"
# Soxta uchta fayl (haqiqiy agent ularni yaratgan deb tasavvur qiling)
head -c 32 /dev/urandom | base64 > "$D/etc/agent.key"; chmod 0600 "$D/etc/agent.key"
printf 'gcm1:Zm9vYmFy' > "$D/var/secrets.bin"; chmod 0600 "$D/var/secrets.bin"
printf '{"schema":1,"cams_epoch":42,"cameras":[]}' > "$D/var/state.json"; chmod 0600 "$D/var/state.json"

tar -czf "$D/bk.tar.gz" -C "$D" etc/agent.key var/secrets.bin var/state.json
R=$(mktemp -d); tar -xzf "$D/bk.tar.gz" -C "$R"

# Uchala fayl qaytdi va mazmuni bir xil
diff "$D/etc/agent.key" "$R/etc/agent.key" && \
diff "$D/var/secrets.bin" "$R/var/secrets.bin" && \
diff "$D/var/state.json" "$R/var/state.json" && echo "TIKLASH OK"
stat -c '%a %n' "$R/etc/agent.key" "$R/var/secrets.bin" "$R/var/state.json"   # 600 kutiladi
rm -rf "$D" "$R"
```

Kutilgan: `TIKLASH OK` va uchala fayl `600`.
