#!/usr/bin/env bash
# make push — barcha o'zgarishlarni (butun repositoriya) default izoh bilan
# commit qilib GitHub'ga yuboradi.
#
# Ishlatish:
#   make push                      # avtomatik izoh: "chore: make push … (sana)"
#   make push MSG="fix(client): …" # o'z izohingiz
#
# Eslatma: repositoriya ildoshi client/ papkasining ustida (server/ ham shu
# repoda) — ya'ni bu qadam server o'zgarishlarini ham yuboradi.
set -euo pipefail

MSG="${1:-}"

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "XATO: bu papka git repositoriyasi emas" >&2
  exit 1
fi
ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"
BR=$(git rev-parse --abbrev-ref HEAD)

echo "== Git: $ROOT (shox: $BR)"
if ! git remote get-url origin >/dev/null 2>&1; then
  echo "XATO: 'origin' remote sozlanmagan. Misol:" >&2
  echo "  git remote add origin https://github.com/<foydalanuvchi>/<repo>.git" >&2
  exit 1
fi
echo "   origin: $(git remote get-url origin)"

if ! timeout 30 git ls-remote --heads origin >/dev/null 2>&1; then
  echo "XATO: origin'ga kira olmadi — tarmoq yoki kirish ma'lumoti tekshirilsin" >&2
  echo "  (gh auth status / git config credential.helper)" >&2
  exit 1
fi

git add -A
if git diff --cached --quiet; then
  echo "   commit qilinadigan o'zgarish yo'q"
else
  if [ -z "$MSG" ]; then
    MSG="chore: make push orqali yuklandi ($(date +%Y-%m-%d\ %H:%M))"
  fi
  git commit -q -m "$MSG"
  echo "   commit: $(git log --oneline -1)"
fi

git fetch -q origin || { echo "XATO: fetch qilib bo'lmadi" >&2; exit 1; }

# Upstream yo'q bo'lsa birinchi push'da bog'laymiz.
if ! git rev-parse --verify -q "origin/$BR" >/dev/null; then
  echo "   origin/$BR yo'q — birinchi push, upstream bog'lanadi"
  git push -q -u origin "HEAD:$BR"
  echo "   OK: $BR shoxini yubordik"
  exit 0
fi

behind=$(git rev-list --count "HEAD..origin/$BR")
if [ "$behind" != "0" ]; then
  echo "   origin'da $behind ta bizda yo'q commit bor — rebase qilinmoqda…"
  if ! git -c rebase.autoStash=true pull --rebase -q origin "$BR"; then
    git rebase --abort >/dev/null 2>&1 || true
    echo "XATO: rebase ziddiyatli — 'git status' ni ko'rib, qo'l bilan birlashtiring" >&2
    exit 1
  fi
  echo "   rebase OK"
fi

ahead=$(git rev-list --count "origin/$BR..HEAD")
if [ "$ahead" = "0" ]; then
  echo "   yuboriladigan commit yo'q (hammasi GitHub'da)"
  exit 0
fi
git push -q origin "HEAD:$BR"
echo "   OK: $ahead commit yuborildi → $(git remote get-url origin) (shox: $BR)"
