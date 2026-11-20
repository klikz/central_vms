#!/usr/bin/env bash
# make update — GitHub'dan (origin) tortib papkani yangilaydi.
#
# Xavfsiz xulq:
#   * joyida yozilmagan o'zgarish bo'lsa ular avtomatik chetga olinadi
#     (autostash) va tortishdan keyin qaytariladi;
#   * faqat tezkor (fast-forward) yangilanish qilinadi — tarixni buzadigan
#     avto-birlashtirish YO'Q; ziddiyat bo'lsa aniq xabar beriladi.
set -euo pipefail

if ! git rev-parse --is-inside-work-tree >/dev/null 2>&1; then
  echo "XATO: bu papka git repositoriyasi emas" >&2
  exit 1
fi
ROOT=$(git rev-parse --show-toplevel)
cd "$ROOT"
BR=$(git rev-parse --abbrev-ref HEAD)

echo "== Git: $ROOT (shox: $BR)"
if ! git remote get-url origin >/dev/null 2>&1; then
  echo "XATO: 'origin' remote sozlanmagan" >&2
  exit 1
fi
echo "   origin: $(git remote get-url origin)"

if ! timeout 30 git fetch -q origin; then
  echo "XATO: origin'dan tortib bo'lmadi (tarmoq/parol?)" >&2
  exit 1
fi

if ! git rev-parse --verify -q "origin/$BR" >/dev/null; then
  echo "XATO: origin'da $BR shoxi yo'q" >&2
  git branch -r | head -10 >&2
  exit 1
fi

before=$(git rev-parse HEAD)
if ! git -c rebase.autoStash=true pull --ff-only -q origin "$BR"; then
  echo "XATO: tezkor yangilab bo'lmadi — joyida yuborilmagan commitlar bor yoki tarix ajralgan." >&2
  echo "  Yechim: avval 'make push' (commitlaringiz rebase bilan yuboriladi)," >&2
  echo "  yoki o'zingiz: git pull --rebase origin $BR" >&2
  exit 1
fi
after=$(git rev-parse HEAD)

if [ "$before" = "$after" ]; then
  echo "   allaqachon eng yangi (o'zgarish yo'q)"
else
  n=$(git rev-list --count "$before..$after")
  echo "   $n ta commit bilan yangilandi:"
  git log --oneline "$before..$after" | head -10 | sed 's/^/     /'
  echo
  echo "   Eslatma: kod o'zgargan bo'lsa qayta quring — make build (va make gate)."
fi
