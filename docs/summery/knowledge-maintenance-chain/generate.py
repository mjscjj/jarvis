from pathlib import Path
from xml.sax.saxutils import escape


WIDTH = 1800
HEIGHT = 1150
OUT = Path(__file__).with_name("jarvis-knowledge-maintenance-chain-v1.svg")

FONT = "Inter, PingFang SC, Microsoft YaHei, sans-serif"
NAVY = "#111827"
MUTED = "#6b7280"
LINE = "#d1d5db"
SOFT_LINE = "#e5e7eb"
BLUE = "#2563eb"
BLUE_TINT = "#eff6ff"
GREEN = "#16a34a"
GREEN_TINT = "#f0fdf4"
PURPLE = "#9333ea"
PURPLE_TINT = "#faf5ff"
ORANGE = "#ea580c"
ORANGE_TINT = "#fff7ed"
GRAY_TINT = "#f9fafb"

defs = []
backgrounds = []
flows = []
nodes = []
labels = []


def a(target, value):
    target.append(value)


def rect(target, x, y, w, h, fill="#ffffff", stroke=LINE, sw=1.5, r=14, dash=None, role=None):
    dash_attr = f' stroke-dasharray="{dash}"' if dash else ""
    role_attr = f' data-graph-role="{role}"' if role else ""
    a(target, f'<rect x="{x}" y="{y}" width="{w}" height="{h}" rx="{r}" fill="{fill}" stroke="{stroke}" stroke-width="{sw}"{dash_attr}{role_attr}/>' )


def line(target, x1, y1, x2, y2, color=LINE, sw=1.5, dash=None):
    dash_attr = f' stroke-dasharray="{dash}"' if dash else ""
    a(target, f'<line x1="{x1}" y1="{y1}" x2="{x2}" y2="{y2}" stroke="{color}" stroke-width="{sw}"{dash_attr}/>' )


def text(target, x, y, value, size=16, color=NAVY, weight=400, anchor="start", spacing=0):
    safe = escape(value)
    a(target, f'<text x="{x}" y="{y}" font-family="{FONT}" font-size="{size}" font-weight="{weight}" fill="{color}" text-anchor="{anchor}" letter-spacing="{spacing}">{safe}</text>')


def multiline(target, x, y, rows, size=14, color=MUTED, weight=400, gap=22, anchor="start"):
    for index, row in enumerate(rows):
        text(target, x, y + index * gap, row, size=size, color=color, weight=weight, anchor=anchor)


def icon(target, cx, cy, kind, color=BLUE, tint=BLUE_TINT):
    a(target, f'<circle cx="{cx}" cy="{cy}" r="20" fill="{tint}"/>')
    if kind == "document":
        a(target, f'<rect x="{cx-8}" y="{cy-10}" width="16" height="20" rx="2" fill="none" stroke="{color}" stroke-width="2"/>')
        line(target, cx-4, cy-4, cx+4, cy-4, color, 2)
        line(target, cx-4, cy+1, cx+4, cy+1, color, 2)
        line(target, cx-4, cy+6, cx+2, cy+6, color, 2)
    elif kind == "cursor":
        line(target, cx-8, cy-7, cx+7, cy-7, color, 2)
        line(target, cx-8, cy, cx+3, cy, color, 2)
        line(target, cx-8, cy+7, cx+8, cy+7, color, 2)
        a(target, f'<circle cx="{cx+8}" cy="{cy-7}" r="3" fill="{color}"/>')
        a(target, f'<circle cx="{cx+4}" cy="{cy}" r="3" fill="{color}"/>')
    elif kind == "agent":
        a(target, f'<circle cx="{cx}" cy="{cy}" r="8" fill="none" stroke="{color}" stroke-width="2"/>')
        line(target, cx, cy-14, cx, cy-10, color, 2)
        line(target, cx, cy+10, cx, cy+14, color, 2)
        line(target, cx-14, cy, cx-10, cy, color, 2)
        line(target, cx+10, cy, cx+14, cy, color, 2)
    elif kind == "database":
        a(target, f'<ellipse cx="{cx}" cy="{cy-7}" rx="10" ry="4" fill="none" stroke="{color}" stroke-width="2"/>')
        a(target, f'<path d="M {cx-10} {cy-7} V {cy+7} C {cx-10} {cy+12}, {cx+10} {cy+12}, {cx+10} {cy+7} V {cy-7}" fill="none" stroke="{color}" stroke-width="2"/>')
        a(target, f'<path d="M {cx-10} {cy} C {cx-10} {cy+5}, {cx+10} {cy+5}, {cx+10} {cy}" fill="none" stroke="{color}" stroke-width="2"/>')
    elif kind == "search":
        a(target, f'<circle cx="{cx-3}" cy="{cy-3}" r="7" fill="none" stroke="{color}" stroke-width="2"/>')
        line(target, cx+2, cy+2, cx+10, cy+10, color, 2.5)
    elif kind == "loop":
        a(target, f'<path d="M {cx-10} {cy+2} A 11 11 0 1 1 {cx+7} {cy+8}" fill="none" stroke="{color}" stroke-width="2"/>')
        a(target, f'<path d="M {cx+6} {cy+2} L {cx+8} {cy+9} L {cx+1} {cy+7}" fill="none" stroke="{color}" stroke-width="2"/>')
    elif kind == "shield":
        a(target, f'<path d="M {cx} {cy-11} L {cx+10} {cy-7} V {cy+1} C {cx+10} {cy+8}, {cx+4} {cy+12}, {cx} {cy+14} C {cx-4} {cy+12}, {cx-10} {cy+8}, {cx-10} {cy+1} V {cy-7} Z" fill="none" stroke="{color}" stroke-width="2"/>')
        a(target, f'<path d="M {cx-5} {cy+1} L {cx-1} {cy+5} L {cx+6} {cy-3}" fill="none" stroke="{color}" stroke-width="2"/>')
    else:
        a(target, f'<circle cx="{cx}" cy="{cy}" r="7" fill="{color}"/>')


def arrow(target, d, color=BLUE, width=3, marker="blue", dashed=False):
    dash_attr = ' stroke-dasharray="8 7"' if dashed else ""
    a(target, f'<path d="{d}" fill="none" stroke="{color}" stroke-width="{width}" stroke-linecap="round" stroke-linejoin="round" marker-end="url(#arrow-{marker})"{dash_attr}/>' )


def section(x, y, w, h, number, title_value, subtitle, color, tint, kind):
    rect(backgrounds, x, y, w, h, fill="#ffffff", stroke=SOFT_LINE, sw=1.5, r=18, role="container")
    a(backgrounds, f'<rect x="{x}" y="{y}" width="{w}" height="62" rx="18" fill="{tint}"/>')
    a(backgrounds, f'<rect x="{x}" y="{y+46}" width="{w}" height="16" fill="{tint}"/>')
    icon(nodes, x+32, y+31, kind, color, "#ffffff")
    text(labels, x+62, y+29, f"{number}  {title_value}", 19, color, 700)
    text(labels, x+62, y+50, subtitle, 12.5, MUTED, 400)


def pill(target, x, y, w, label_value, color, tint, stroke=None):
    rect(target, x, y, w, 30, fill=tint, stroke=stroke or tint, sw=1, r=15)
    text(labels, x+w/2, y+20, label_value, 12.5, color, 600, anchor="middle")


def mini_card(x, y, w, h, title_value, rows, accent=BLUE, tint="#ffffff", dashed=None):
    rect(nodes, x, y, w, h, fill=tint, stroke=accent, sw=1.6, r=12, dash=dashed)
    a(nodes, f'<rect x="{x}" y="{y}" width="5" height="{h}" rx="2.5" fill="{accent}"/>')
    text(labels, x+18, y+27, title_value, 16, NAVY, 700)
    multiline(labels, x+18, y+50, rows, 12.5, MUTED, 400, 19)


a(defs, '<defs>')
for marker_name, marker_color in [("blue", BLUE), ("green", GREEN), ("purple", PURPLE), ("orange", ORANGE), ("gray", "#94a3b8")]:
    a(defs, f'<marker id="arrow-{marker_name}" markerWidth="10" markerHeight="10" refX="8" refY="3" orient="auto" markerUnits="strokeWidth"><path d="M0,0 L0,6 L9,3 z" fill="{marker_color}"/></marker>')
a(defs, '<filter id="soft-shadow" x="-20%" y="-20%" width="140%" height="140%"><feDropShadow dx="0" dy="3" stdDeviation="6" flood-color="#0f172a" flood-opacity="0.08"/></filter>')
a(defs, '</defs>')

# Canvas and header
a(backgrounds, f'<rect width="{WIDTH}" height="{HEIGHT}" fill="#ffffff" data-graph-role="background"/>')
a(backgrounds, '<circle cx="1690" cy="55" r="150" fill="#eff6ff" opacity="0.8"/>')
a(backgrounds, '<circle cx="1755" cy="115" r="72" fill="#f0fdf4" opacity="0.8"/>')
text(labels, 52, 57, "Jarvis 知识库维护链路", 36, NAVY, 750)
text(labels, 52, 92, "从增量证据到可复用的当前世界：当前状态可更新、历史证据可追溯、失败批次可重放", 17, MUTED, 400)

# Legend
legend_y = 55
for lx, color, label_value, dashed in [
    (1165, BLUE, "证据流", False),
    (1290, GREEN, "当前认知 / 读取", False),
    (1482, PURPLE, "历史审计", False),
    (1615, "#94a3b8", "控制 / 重放", True),
]:
    line(labels, lx, legend_y, lx+28, legend_y, color, 3, "7 5" if dashed else None)
    text(labels, lx+36, legend_y+5, label_value, 12.5, NAVY, 500)

# Top principle strip
rect(backgrounds, 50, 112, 1700, 40, fill=GRAY_TINT, stroke=SOFT_LINE, sw=1, r=12)
text(labels, 900, 138, "维护目标  =  当前可信状态（Summary）  +  时间证据（Fact）  +  认知审计（PageRevision）", 15, NAVY, 650, anchor="middle")

# Main sections
section(50, 170, 250, 555, "01", "增量证据", "外部变化与自身行动结果", BLUE, BLUE_TINT, "document")
section(340, 170, 280, 555, "02", "可靠增量接入", "独立游标、整条批处理、失败重放", BLUE, BLUE_TINT, "cursor")
section(660, 170, 560, 555, "03", "FactEngine / 世界维护 Agent", "读当前认知，判断新证据改变了什么", ORANGE, ORANGE_TINT, "agent")
section(1260, 170, 490, 555, "04", "One World Model", "实体索引 + 当前状态 + 时间证据 + 审计", GREEN, GREEN_TINT, "database")

# Primary corridor arrows first
arrow(flows, "M 300 447 H 340", BLUE, 3, "blue")
arrow(flows, "M 620 447 H 660", BLUE, 3, "blue")
text(labels, 320, 431, "原样进入", 11.5, BLUE, 600, anchor="middle")
text(labels, 640, 431, "一次变化批", 11.5, BLUE, 600, anchor="middle")

# Source cards
mini_card(72, 235, 206, 66, "Message", ["消息 / 会议 / 文档变化"], BLUE, "#ffffff")
mini_card(72, 320, 206, 66, "TodoEvent", ["线索准入与观察变化"], BLUE, "#ffffff")
mini_card(72, 405, 206, 66, "TaskEvent", ["目标、状态与进度变化"], BLUE, "#ffffff")
mini_card(72, 490, 206, 82, "ExecutionRun / effects", ["执行结果与真实副作用", "重新成为下一轮证据"], BLUE, "#ffffff")
mini_card(72, 590, 206, 74, "Resource", ["元数据已进入；通用解析", "仍在建设"], "#94a3b8", GRAY_TINT, dashed="6 5")
text(labels, 175, 694, "原始证据保留来源与完整语义", 12.5, BLUE, 600, anchor="middle")

# Ingestion cards
mini_card(365, 235, 230, 72, "独立来源游标", ["Message / Todo / Task", "各自推进，互不遮蔽"], BLUE, BLUE_TINT)
mini_card(365, 328, 230, 72, "整条证据成批", ["先按来源读取，再组成", "一次 WORLD_CHANGES"], BLUE, "#ffffff")
mini_card(365, 421, 230, 72, "预算控制", ["超预算就缩小批次", "不截断单条证据"], BLUE, "#ffffff")
mini_card(365, 514, 230, 72, "冻结本轮输入", ["一个变化批，只启动", "一个维护 Agent"], BLUE, "#ffffff")
rect(nodes, 365, 610, 230, 78, fill=GRAY_TINT, stroke="#94a3b8", sw=1.4, r=12)
icon(nodes, 389, 649, "loop", "#64748b", "#ffffff")
text(labels, 416, 637, "成功后推进游标", 13, NAVY, 650)
text(labels, 416, 660, "失败停留，下轮原批重放", 12.5, MUTED, 500)

# FactEngine read steps
mini_card(690, 235, 225, 78, "1  定位受影响实体", ["list-pages：人物 / 项目 /", "关键事项 / 群 / 资源"], ORANGE, "#ffffff")
mini_card(950, 235, 240, 78, "2  读取完整当前认知", ["get-page：加载整页 Summary", "来源提示不是实体白名单"], ORANGE, "#ffffff")
arrow(flows, "M 915 274 H 950", ORANGE, 2.5, "orange")

# Semantic comparison core
rect(nodes, 690, 346, 232, 278, fill=ORANGE_TINT, stroke=ORANGE, sw=2, r=18)
icon(nodes, 806, 388, "search", ORANGE, "#ffffff")
text(labels, 806, 429, "语义对照", 19, ORANGE, 750, anchor="middle")
text(labels, 806, 458, "新证据", 13.5, NAVY, 650, anchor="middle")
text(labels, 806, 481, "×", 18, ORANGE, 700, anchor="middle")
text(labels, 806, 505, "当前 Summary", 13.5, NAVY, 650, anchor="middle")
line(nodes, 718, 526, 894, 526, "#fed7aa", 1.5)
multiline(labels, 718, 552, ["判断它是否改变长期认知", "允许工具调查，语义由 Agent 决定", "无有效变化时 NOTHING 也是正确结果"], 12.3, MUTED, 500, 22)
arrow(flows, "M 1070 313 V 329 H 806 V 346", ORANGE, 2.5, "orange")

# Semantic branches
mini_card(960, 346, 230, 76, "A  噪声 / 重复", ["NOTHING：保持当前状态"], "#94a3b8", GRAY_TINT)
mini_card(960, 457, 230, 82, "B  新事实 / 状态前进", ["追加 Fact，并更新 Summary", "时间证据不覆盖"], BLUE, BLUE_TINT)
mini_card(960, 574, 230, 96, "C  冲突 / 认知过期", ["改写 Summary；旧认知自动", "归档为 PageRevision"], PURPLE, PURPLE_TINT)
arrow(flows, "M 922 386 H 960", "#94a3b8", 2.2, "gray")
arrow(flows, "M 922 496 H 960", BLUE, 2.2, "blue")
arrow(flows, "M 922 614 H 960", PURPLE, 2.2, "purple")

# Branches to stores
arrow(flows, "M 1190 384 H 1260", "#94a3b8", 2.2, "gray", dashed=True)
arrow(flows, "M 1190 497 H 1260", BLUE, 2.6, "blue")
arrow(flows, "M 1190 622 H 1260", PURPLE, 2.6, "purple")
text(labels, 1225, 369, "保持", 11.5, "#64748b", 600, anchor="middle")
text(labels, 1225, 482, "写入", 11.5, BLUE, 600, anchor="middle")
text(labels, 1225, 607, "归档", 11.5, PURPLE, 600, anchor="middle")

# World model contents
rect(nodes, 1285, 232, 440, 76, fill="#ffffff", stroke=GREEN, sw=1.5, r=12)
text(labels, 1308, 258, "实体索引与关系", 16, GREEN, 700)
pill(nodes, 1308, 269, 58, "人", GREEN, GREEN_TINT)
pill(nodes, 1374, 269, 68, "项目", GREEN, GREEN_TINT)
pill(nodes, 1450, 269, 80, "关键事项", GREEN, GREEN_TINT)
pill(nodes, 1538, 269, 58, "群", GREEN, GREEN_TINT)
pill(nodes, 1604, 269, 72, "资源", GREEN, GREEN_TINT)

mini_card(1285, 335, 440, 76, "Summary  ·  现在应该相信什么", ["每个长期实体一页；当前状态可整体改写"], GREEN, GREEN_TINT)
mini_card(1285, 452, 440, 82, "Fact  ·  发生过什么", ["时间化证据只追加；支撑当前认知与追溯"], BLUE, BLUE_TINT)
mini_card(1285, 574, 440, 96, "PageRevision  ·  过去曾怎样理解", ["Summary 改写前自动归档；它是认知审计", "不是另一条 Fact"], PURPLE, PURPLE_TINT)
rect(nodes, 1285, 684, 440, 26, fill=GRAY_TINT, stroke=SOFT_LINE, sw=1, r=8)
text(labels, 1505, 702, "机器护栏：引用检查 · CAS · 整页读写 · 写后回读", 12, NAVY, 600, anchor="middle")

# Internal store relationships
arrow(flows, "M 1685 452 V 421 H 1685 V 411", GREEN, 2, "green")
arrow(flows, "M 1705 411 H 1735 V 574 H 1725", PURPLE, 2, "purple")

# Lower closed-loop section
rect(backgrounds, 50, 780, 1700, 278, fill=GRAY_TINT, stroke=SOFT_LINE, sw=1.5, r=18, role="container")
icon(nodes, 82, 812, "loop", GREEN, GREEN_TINT)
text(labels, 112, 818, "05  按需读取与结果回流", 20, NAVY, 750)
text(labels, 426, 818, "默认读当前状态；需要解释或追责时再展开历史证据", 13, MUTED, 400)

# Lower flow arrows, routed in their own corridors
arrow(flows, "M 1510 725 V 810 H 1480 V 838", GREEN, 3, "green")
arrow(flows, "M 1125 928 H 820", GREEN, 3, "green")
arrow(flows, "M 470 928 H 390", ORANGE, 3, "orange")
arrow(flows, "M 80 928 H 28 V 448 H 50", BLUE, 2.5, "blue", dashed=True)
arrow(flows, "M 735 856 V 756 H 1232 V 715 H 1260", GREEN, 2.5, "green", dashed=True)
text(labels, 1540, 792, "按需读取", 11.5, GREEN, 600)
text(labels, 950, 912, "当前认知 + 必要历史", 11.5, GREEN, 600, anchor="middle")
text(labels, 430, 912, "执行", 11.5, ORANGE, 600, anchor="middle")
text(labels, 36, 748, "结果成为新证据", 11.5, BLUE, 600, anchor="start")
text(labels, 985, 749, "M5 核实过期认知后，可沿同一 update-page 入口修正", 11.5, GREEN, 600, anchor="middle")

# Outcome card
rect(nodes, 80, 856, 310, 144, fill="#ffffff", stroke=BLUE, sw=1.7, r=14)
icon(nodes, 112, 891, "document", BLUE, BLUE_TINT)
text(labels, 142, 895, "行动结果", 18, NAVY, 700)
text(labels, 104, 929, "TaskEvent / ExecutionRun / effects", 13.5, BLUE, 650)
multiline(labels, 104, 957, ["外部现实被改变后，再次沉淀为证据", "世界模型因此持续校正，而非一次性建库"], 12.5, MUTED, 400, 21)

# Agent consumers
rect(nodes, 470, 856, 350, 144, fill="#ffffff", stroke=ORANGE, sw=1.7, r=14)
icon(nodes, 502, 891, "agent", ORANGE, ORANGE_TINT)
text(labels, 532, 895, "决策与执行 Agent", 18, NAVY, 700)
pill(nodes, 500, 920, 84, "M3 准入", ORANGE, ORANGE_TINT)
pill(nodes, 594, 920, 92, "M5 执行", ORANGE, ORANGE_TINT)
pill(nodes, 696, 920, 94, "主动巡检", ORANGE, ORANGE_TINT)
multiline(labels, 500, 972, ["上下文服务当前阶段目标；不是把全库塞进 Prompt"], 12.5, MUTED, 400, 20)

# Progressive retrieval
rect(nodes, 1125, 838, 390, 180, fill="#ffffff", stroke=GREEN, sw=1.7, r=14)
icon(nodes, 1157, 875, "search", GREEN, GREEN_TINT)
text(labels, 1187, 879, "渐进式检索", 18, NAVY, 700)
pill(nodes, 1152, 906, 94, "list-pages", GREEN, GREEN_TINT)
text(labels, 1262, 926, "→", 18, GREEN, 700, anchor="middle")
pill(nodes, 1278, 906, 88, "get-page", GREEN, GREEN_TINT)
text(labels, 1382, 926, "→", 18, GREEN, 700, anchor="middle")
pill(nodes, 1398, 906, 90, "list-facts", GREEN, GREEN_TINT)
multiline(labels, 1152, 970, ["先找实体，再读完整 Summary；只有需要解释、冲突", "或追责时，才按实体和时间展开 Fact / PageRevision"], 12.4, MUTED, 400, 21)

# Footer boundary and honest current gaps
rect(backgrounds, 50, 1080, 790, 42, fill="#0f172a", stroke="#0f172a", sw=1, r=12)
icon(nodes, 78, 1101, "shield", "#38bdf8", "#172554")
text(labels, 108, 1107, "边界：FactEngine 只维护内部认知，不创建 Task，也不对外发送消息", 13.5, "#ffffff", 600)
rect(backgrounds, 860, 1080, 890, 42, fill=ORANGE_TINT, stroke="#fdba74", sw=1, r=12)
a(nodes, '<circle cx="888" cy="1101" r="6" fill="#f59e0b"/>')
text(labels, 906, 1107, "当前收口项：Fact 证据指针需从可选字段升级为强追溯；Resource 通用解析尚未完全闭环", 13.2, "#9a3412", 600)

svg = []
a(svg, f'<svg xmlns="http://www.w3.org/2000/svg" width="{WIDTH}" height="{HEIGHT}" viewBox="0 0 {WIDTH} {HEIGHT}">')
for layer in (defs, backgrounds, flows, nodes, labels):
    svg.extend(layer)
a(svg, '</svg>')
OUT.write_text("\n".join(svg), encoding="utf-8")
print(OUT)
