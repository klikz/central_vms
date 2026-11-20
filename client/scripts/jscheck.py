#!/usr/bin/env python3
"""app.js uchun yengil sintaks tekshiruvi (node yo'q muhit uchun).

Asosiy nishon: o'zbek apostrofi (' bilan yozilgan so'zlar) bir qadrqli JS
satrini uzib qo'yishi — natijada butun skript ishlamaydi va UI bo'sh qoladi
(§17). Shu xato turini ushlaydi; qavs muvozanatini ham tekshiradi.

Cheklov: regex literal'lar qo'llab-quvvatlanmaydi (app.js da ishlatmang —
uning o'rniga split/join), chunki ularni oddiy skaner bilan ajratib bo'lmaydi.
"""
import sys

CODE, TEMPLATE = "code", "template"


def check(path):
    src = open(path, encoding="utf-8").read()
    i, n = 0, len(src)
    line = 1
    errs = []
    stack = [[CODE, 0]]  # [rejim, qavs chuqurligi]
    brackets = []        # (qavs, qator) — faqat CODE rejimida

    def cur():
        return stack[-1]

    while i < n:
        c = src[i]
        if c == "\n":
            line += 1
            i += 1
            continue

        mode = cur()[0]
        if mode == TEMPLATE:
            if c == "\\":
                i += 2
                continue
            if c == "`":
                stack.pop()
                i += 1
                continue
            if c == "$" and i + 1 < n and src[i + 1] == "{":
                cur()[1] += 1
                stack.append([CODE, 0])
                i += 2
                continue
            i += 1
            continue

        # --- CODE rejimi ---
        if c == "/" and i + 1 < n and src[i + 1] == "/":
            while i < n and src[i] != "\n":
                i += 1
            continue
        if c == "/" and i + 1 < n and src[i + 1] == "*":
            i += 2
            while i + 1 < n and not (src[i] == "*" and src[i + 1] == "/"):
                if src[i] == "\n":
                    line += 1
                i += 1
            i += 2
            continue
        if c in "'\"":
            quote, start_line, start_idx = c, line, i
            i += 1
            closed = False
            while i < n:
                if src[i] == "\\":
                    if i + 1 < n and src[i + 1] == "\n":
                        line += 1
                    i += 2
                    continue
                if src[i] == quote:
                    closed = True
                    break
                if src[i] == "\n":
                    errs.append(f"{path}:{start_line}: satr yopilmagan (newline)")
                    break
                i += 1
            if not closed:
                errs.append(f"{path}:{start_line}: satr yopilmagan (EOF)")
                return errs
            i += 1
            j = i
            while j < n and src[j] in " \t":
                j += 1
            if j < n and (src[j].isalpha() or src[j] in "_$"):
                seg = src[max(0, src.rfind("\n", 0, start_idx) + 1):min(n, i + 12)]
                errs.append(f"{path}:{start_line}: bir qadrqli satr noto'g'ri yopildi "
                            f"(o'zbek apostrofi?): …{seg.strip()[:70]}")
            continue
        if c == "`":
            stack.append([TEMPLATE, 0])
            i += 1
            continue
        in_expr = len(stack) > 1 and stack[-1][0] == CODE
        if c == "{":
            if in_expr:
                stack[-1][1] += 1
            else:
                brackets.append((c, line))
        elif c in "([":
            brackets.append((c, line))
        elif c == "}" and in_expr:
            # ${ … } ichidagi qavs: chuqurlik hisobi (brackets ro'yxatiga kirmaydi)
            if stack[-1][1] == 0:
                stack.pop()  # ifoda tugadi → template rejimiga qaytamiz
            else:
                stack[-1][1] -= 1
        elif c in ")]}":
            pair = {")": "(", "]": "[", "}": "{"}[c]
            if not brackets:
                errs.append(f"{path}:{line}: ortiqcha yopuvchi '{c}'")
            elif brackets[-1][0] != pair:
                errs.append(f"{path}:{line}: qavs mos emas: '{c}' ({brackets[-1][1]} dagi '{brackets[-1][0]}')")
                brackets.pop()
            else:
                brackets.pop()
        i += 1

    if len(stack) > 1:
        errs.append(f"{path}: template literal yopilmagan")
    for q, ln in brackets:
        errs.append(f"{path}:{ln}: '{q}' yopilmagan")
    return errs


def main():
    bad = 0
    for path in sys.argv[1:]:
        for e in check(path)[:12]:
            print(e)
            bad += 1
    if bad:
        print(f"jscheck: {bad} xato")
        return 1
    print("jscheck: OK")
    return 0


if __name__ == "__main__":
    sys.exit(main())
