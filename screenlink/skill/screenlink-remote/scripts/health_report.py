"""Turn a findings JSON (see references/report-format.md) into an Excel health report.

    python health_report.py findings.json report.xlsx

Sheets: Summary (who/what/when, licence, root cause, fix, counts) and Checks
(one row per check, colour-coded by status). Needs openpyxl.
"""
import json
import sys

try:
    from openpyxl import Workbook
    from openpyxl.styles import Alignment, Border, Font, PatternFill, Side
    from openpyxl.utils import get_column_letter
except ImportError:
    sys.exit("openpyxl is needed: pip install openpyxl")

NAVY, CYAN = "10498B", "00AEEF"
STATUS = {  # fill, font
    "OK": ("DCFCE7", "166534"),
    "WARN": ("FEF3C7", "92400E"),
    "PROBLEM": ("FEE2E2", "991B1B"),
    "UNKNOWN": ("E5E7EB", "374151"),
}
thin = Side(style="thin", color="DBE8F3")
BOX = Border(left=thin, right=thin, top=thin, bottom=thin)
WRAP = Alignment(wrap_text=True, vertical="top")


def status_of(s):
    s = str(s or "UNKNOWN").upper()
    return s if s in STATUS else "UNKNOWN"


def header(ws, row, cols):
    for i, name in enumerate(cols, 1):
        c = ws.cell(row=row, column=i, value=name)
        c.font = Font(bold=True, color="FFFFFF")
        c.fill = PatternFill("solid", fgColor=NAVY)
        c.border = BOX
        c.alignment = Alignment(vertical="center")


def paint(cell, st):
    fill, font = STATUS[st]
    cell.fill = PatternFill("solid", fgColor=fill)
    cell.font = Font(bold=True, color=font)


def main(src, dst):
    with open(src, encoding="utf-8") as f:
        d = json.load(f)
    checks = d.get("checks", [])
    wb = Workbook()

    # ---- Summary ----
    ws = wb.active
    ws.title = "Summary"
    ws["A1"] = d.get("title", "Health check")
    ws["A1"].font = Font(bold=True, size=16, color=NAVY)
    ws["A2"] = "ScreenLink"
    ws["A2"].font = Font(bold=True, color=CYAN)
    r = 4
    for label, key in (("Client", "client"), ("Host", "host"), ("Engineer", "engineer"),
                       ("Date", "date"), ("Method", "method")):
        if d.get(key):
            ws.cell(row=r, column=1, value=label).font = Font(bold=True)
            ws.cell(row=r, column=2, value=str(d[key]))
            r += 1

    counts = {k: sum(1 for c in checks if status_of(c.get("status")) == k) for k in STATUS}
    r += 1
    header(ws, r, ["Status", "Checks"])
    for k in STATUS:
        r += 1
        a = ws.cell(row=r, column=1, value=k)
        paint(a, k)
        a.border = BOX
        ws.cell(row=r, column=2, value=counts[k]).border = BOX

    lic = d.get("licence")
    if lic:
        r += 2
        header(ws, r, ["Licence", ""])
        for label, key in (("Type", "type"), ("Expiry", "expiry"), ("Days left", "days_left"),
                           ("Usage", "usage"), ("Status", "status")):
            if key in lic:
                r += 1
                ws.cell(row=r, column=1, value=label).font = Font(bold=True)
                v = ws.cell(row=r, column=2, value=lic[key])
                if key == "status":
                    paint(v, status_of(lic[key]))

    if d.get("root_cause"):
        r += 2
        ws.cell(row=r, column=1, value="Root cause").font = Font(bold=True, color=NAVY)
        c = ws.cell(row=r, column=2, value=d["root_cause"])
        c.alignment = WRAP
        ws.row_dimensions[r].height = 45
    if d.get("fix"):
        r += 2
        ws.cell(row=r, column=1, value="Fix (needs OK)").font = Font(bold=True, color=NAVY)
        for i, step in enumerate(d["fix"], 1):
            c = ws.cell(row=r, column=2, value=f"{i}. {step}")
            c.alignment = WRAP
            r += 1
    ws.column_dimensions["A"].width = 18
    ws.column_dimensions["B"].width = 90

    # ---- Checks ----
    wc = wb.create_sheet("Checks")
    cols = ["Area", "Check", "Status", "Evidence", "Note"]
    header(wc, 1, cols)
    order = {"PROBLEM": 0, "WARN": 1, "UNKNOWN": 2, "OK": 3}
    for i, c in enumerate(sorted(checks, key=lambda c: order[status_of(c.get("status"))]), 2):
        st = status_of(c.get("status"))
        vals = [c.get("area", ""), c.get("check", ""), st, c.get("evidence", ""), c.get("note", "")]
        for j, v in enumerate(vals, 1):
            cell = wc.cell(row=i, column=j, value=v)
            cell.border = BOX
            cell.alignment = WRAP
        paint(wc.cell(row=i, column=3), st)
    for j, w in enumerate([14, 34, 11, 50, 40], 1):
        wc.column_dimensions[get_column_letter(j)].width = w
    wc.freeze_panes = "A2"
    wc.auto_filter.ref = f"A1:E{max(len(checks) + 1, 2)}"

    wb.save(dst)
    print(f"saved {dst}: {len(checks)} checks "
          f"({counts['PROBLEM']} problem, {counts['WARN']} warn, {counts['OK']} ok)")


if __name__ == "__main__":
    if len(sys.argv) != 3:
        sys.exit("usage: python health_report.py findings.json report.xlsx")
    main(sys.argv[1], sys.argv[2])
