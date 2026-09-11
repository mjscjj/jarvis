from __future__ import annotations

import html
import json
import re
import subprocess
import xml.etree.ElementTree as ET
from pathlib import Path


ROOT = Path("docs/summery/task-45-prd-review-trim")
EVIDENCE = ROOT / "evidence"


def esc(text: str) -> str:
    return html.escape(text, quote=True)


def link(url: str, text: str) -> str:
    return f'<a href="{esc(url)}">{esc(text)}</a>'


def p(text: str) -> str:
    return f"<p>{text}</p>"


def li(text: str) -> str:
    return f"<li>{text}</li>"


def ul(items: list[str]) -> str:
    return "<ul>" + "".join(li(item) for item in items) + "</ul>"


def table(headers: list[str], rows: list[list[str]]) -> str:
    head = "".join(f"<th><p>{esc(h)}</p></th>" for h in headers)
    body = "".join(
        "<tr>" + "".join(f"<td><p>{cell}</p></td>" for cell in row) + "</tr>"
        for row in rows
    )
    return f"<table><thead><tr>{head}</tr></thead><tbody>{body}</tbody></table>"


DOCS = [
    {
        "token": "LC7YdQLpIoPVmcxF99FmGNZly5g",
        "title": "PRD Review｜LIVE Push CTR｜精简版",
        "xml": (
            "<title>PRD Review｜LIVE Push CTR｜精简版</title>"
            "<h1>结论</h1>"
            + p(
                "方向可以继续：用 LIVE 自有 CTR 模型争取 Push 强触达资格。上线前最关键的是把 DS 实验方案、Full 组护栏和异常兜底写成可执行口径。"
            )
            + "<h1>关键问题</h1>"
            + ul(
                [
                    "DS 还未正式承接实验方案。会议评论要求“让 DS 出；提个需求”，建议把阈值表、样本量、T+15d 成熟口径列为正式实验前置。",
                    "Full 组已知达不到 5% CTR，但 PRD 只写 0.2% 规模，未写曝光上限、负反馈阈值和回退 Owner；建议补中台确认记录和停止条件。",
                    "模型失败、特征缺失、Push 回传缺失时只写“弱触达或停止”，建议补异常决策表，避免实验样本和真实触达混杂。",
                ]
            )
            + "<h1>依据</h1>"
            + p(
                "原 PRD："
                + link(
                    "https://bytedance.my.larkoffice.com/docx/JzNOdClz4o388RxZRQSmubiwyTh",
                    "LIVE Push CTR模型与强触达恢复",
                )
                + "；会议状态：通过；读取版本：revision 752。未覆盖：外部算法文档、Push 中台正式口径和完整合规材料。"
            )
        ),
    },
    {
        "token": "VjZPddXcNokSp9xpH3pmcMXAylh",
        "title": "PRD Review｜B端报告内容迁移｜精简版",
        "xml": (
            "<title>PRD Review｜B端报告内容迁移｜精简版</title>"
            "<h1>结论</h1>"
            + p(
                "方向合理：停止低效的 B 端报告预生产，把内部评估信息迁移到优质主播详情页，切片和口播分析交给 Rena 按需生成。上线前需补齐入口、分享边界和 Rena Skill 依赖。"
            )
            + "<h1>关键问题</h1>"
            + ul(
                [
                    "目标用户和入口仍散在评论里，正文未固化。建议区分主播详情页 CM 与调优工作台 CM，分别写清迁移后入口、核心动作和埋点。",
                    "“停止生产 B 端报告”与“保留分享给主播”并存，但分享内容来源未定。建议明确只分享 C 端报告、详情页快照或新分享页，且内部评估信息不得给主播侧可见。",
                    "Rena 口播分析 Skill 仍是设计态。若作为上线硬依赖，需补 P0 能力、数据源、超时/失败提示和联调验收；否则需说明先迁移静态内容的降级方案。",
                ]
            )
            + "<h1>依据</h1>"
            + p(
                "原 PRD："
                + link(
                    "https://bytedance.larkoffice.com/wiki/OM6lwdQsxi4EndkSN1lcyOHSnyb",
                    "B端报告内容迁移",
                )
                + "；会议状态：通过 with todo；读取版本：revision 2937。未覆盖：嵌入表格、完整图片、线上入口实现。"
            )
        ),
    },
    {
        "token": "UynidBTYpolI8rx005AmyIZnyId",
        "title": "PRD Review｜铁魔杖文件上传｜精简版",
        "xml": (
            "<title>PRD Review｜铁魔杖文件上传｜精简版</title>"
            "<h1>结论</h1>"
            + p(
                "需求边界清楚：为新版铁魔杖补回多任务文件上传能力。主要交付风险在模板字段、校验结果流转、审批替换和执行前二次校验。"
            )
            + "<h1>关键问题</h1>"
            + ul(
                [
                    "PRD 未固化上传模板字段。建议补字段名、必填、枚举、时区/时间格式、最大 100 个 VID、示例值和错误文案，避免运营与研发口径不一致。",
                    "Fail/Warn/Success 后续动作不够清楚。建议规定 Fail 不可提交且可下载错误清单，Warn 需确认后提交，审批只包含 Success 和已确认 Warn。",
                    "重新上传会替换原视频并重走审批，但未定义旧审批、文件版本和视频列表关系。建议新增状态流，并在执行前对配额、冲突、可加热状态做二次校验。",
                ]
            )
            + "<h1>依据</h1>"
            + p(
                "原 PRD："
                + link(
                    "https://bytedance.larkoffice.com/wiki/WY6WwFv4BipY3gkFMjaclHzGn5g",
                    "铁魔杖新版本增加文件上传功能",
                )
                + "；会议状态：通过；读取版本：revision 1695。未覆盖：配置指南、模板文件、审批系统细节和图片。"
            )
        ),
    },
    {
        "token": "XTKAdu2JgoRsmdxg2rnmRqYiyl2",
        "title": "PRD Review｜Rena 主播人群包拆分｜精简版",
        "xml": (
            "<title>PRD Review｜Rena 主播人群包拆分｜精简版</title>"
            "<h1>结论</h1>"
            + p(
                "方向可继续推进；规则图已说明报名型/非报名型的优先级和沉默用户不触达。真正需要补的是发送前状态复核、拆包触发方、重复拆分归属和本期容量承诺。"
            )
            + "<h1>关键问题</h1>"
            + ul(
                [
                    "PRD 一处要求“拆包/发送触发时刻实时准出”，但时序图只画拆包时查状态。建议明确发送前由 DP、Campaign 还是 Message 复核，失败或超时时如何处理。",
                    "正文写“DP 向发起方请求后置拆包任务”，时序图又是 Reach/Campaign 发起方到 DP。建议统一唯一入口，并写清异步完成后谁通知、谁启动分包发送。",
                    "母包到子包是一对多，但未说明同母包多轮拆分、重试和部分失败时 Message 消费哪一轮。建议引入批次标识或限制本期只支持一次拆分。",
                    "图示出现 1 亿量级，但扩容 PRD 短期只到 1000 万、1 亿属中长期。建议把本期承诺写成已联合验证量级，超限时在受理阶段明确拒绝或分批。",
                ]
            )
            + "<h1>依据</h1>"
            + p(
                "原 PRD："
                + link(
                    "https://bytedance.larkoffice.com/wiki/RUjYw8XT7iCjNHk5BVCckEPTn2c",
                    "提供主播人群包拆包能力给 Rena 侧使用",
                )
                + "；会议状态：通过；原 PRD revision 1062，扩容 PRD revision 29。未覆盖：真实部署、接口实现、吞吐和错发率测试。"
            )
        ),
    },
    {
        "token": "HhmRd3m4bo4mbdxS0r0mmrVZy0b",
        "title": "PRD Review｜主播类型与运营绑定解耦｜精简版",
        "xml": (
            "<title>PRD Review｜主播类型与运营绑定解耦｜精简版</title>"
            "<h1>结论</h1>"
            + p(
                "需求方向成立：主播类型按公会绑定计算，自营运营关系用新增字段承接。上线前最需要修正字段描述矛盾，并补下游报表、历史回填和权限边界。"
            )
            + "<h1>关键问题</h1>"
            + ul(
                [
                    "字段章节同时写“不新增字段”和“新增是否绑定运营”，与会议 todo 冲突。建议统一为调整 anchor_type_v2，新增 is_operator_bound，并说明 operator 写入/删除规则。",
                    "下游报表影响未展开。主播类型从三值变两值后，原“自营”口径可能失真；建议列消费方清单，逐项决定使用 anchor_type_v2、is_operator_bound 还是运营绑定区域。",
                    "存量主播和多底表一致性未闭合。建议明确历史自营关系是否回填、回填来源和时间点，并定义实时表、离线表、导出和前端展示的延迟与冲突处理。",
                    "TCN 主播被自营 CM 查看/导出的权限边界不够清楚。建议补权限矩阵：可见字段、可导出字段、可操作动作和是否需要假名化。",
                ]
            )
            + "<h1>依据</h1>"
            + p(
                "原 PRD："
                + link(
                    "https://bytedance.larkoffice.com/wiki/UT90wJPHsi7jIBkKHFscMXzinxf",
                    "主播类型与运营绑定关系单向解耦",
                )
                + "；会议状态：通过 with todo；读取版本：revision 2857。未覆盖：旧 PRD、底表 schema、报表消费方和权限配置。"
            )
        ),
    },
    {
        "token": "YYKmdUevToWBYYxBZOvmO0ioyGf",
        "title": "Arena PRD Review 汇总｜2026-09-09 组内评审｜精简版",
        "xml": (
            "<title>Arena PRD Review 汇总｜2026-09-09 组内评审｜精简版</title>"
            "<h1>总评</h1>"
            + p(
                "本次 5 份 PRD 均可继续推进；最值得 Principal 关注的是 2 个 with todo 需求，以及跨需求反复出现的数据口径、状态流转、异常兜底、权限可见范围和上线验收边界。"
            )
            + "<h1>报告入口</h1>"
            + table(
                ["PRD", "会议状态", "最关键结论", "报告"],
                [
                    [
                        esc("LIVE Push CTR"),
                        esc("通过"),
                        esc("DS 实验方案、Full 组护栏和模型异常兜底需作为正式实验前置。"),
                        link("https://bytedance.my.larkoffice.com/docx/LC7YdQLpIoPVmcxF99FmGNZly5g", "打开"),
                    ],
                    [
                        esc("B端报告内容迁移"),
                        esc("通过 with todo"),
                        esc("分享内容来源、Rena 口播 Skill 依赖、旧报告下线兼容需固化。"),
                        link("https://bytedance.my.larkoffice.com/docx/VjZPddXcNokSp9xpH3pmcMXAylh", "打开"),
                    ],
                    [
                        esc("铁魔杖文件上传"),
                        esc("通过"),
                        esc("上传模板、Warn/Fail 流转、审批替换和执行前二次校验需补齐。"),
                        link("https://bytedance.my.larkoffice.com/docx/UynidBTYpolI8rx005AmyIZnyId", "打开"),
                    ],
                    [
                        esc("Rena 主播人群包拆分"),
                        esc("通过"),
                        esc("发送前状态复核、拆包触发方、重复拆分和本期量级承诺需确认。"),
                        link("https://bytedance.my.larkoffice.com/docx/XTKAdu2JgoRsmdxg2rnmRqYiyl2", "打开"),
                    ],
                    [
                        esc("主播类型与运营绑定解耦"),
                        esc("通过 with todo"),
                        esc("新增字段口径、下游报表、历史回填和 TCN 可见权限需明确。"),
                        link("https://bytedance.my.larkoffice.com/docx/HhmRd3m4bo4mbdxS0r0mmrVZy0b", "打开"),
                    ],
                ],
            )
            + "<h1>范围</h1>"
            + p(
                "会议证据："
                + link(
                    "https://applink.feishu.cn/client/thread/open?open_chat_id=oc_43d3b4c42308cc09705a09768650734d&open_thread_id=omt_19c7d40625cf0765",
                    "2026/09/10 Arena 产品组内结论消息",
                )
                + "。本次只精简既有 Review 报告和汇总，不修改源 PRD、Meego、群消息或 OKR。"
            )
        ),
    },
]


def run(args: list[str], output_path: Path | None = None) -> dict:
    result = subprocess.run(
        ["lark-cli", *args, "--as", "user", "--format", "json"],
        text=True,
        capture_output=True,
    )
    payload = result.stdout if result.returncode == 0 else json.dumps(
        {
            "exit_code": result.returncode,
            "stdout": result.stdout,
            "stderr": result.stderr,
        },
        ensure_ascii=False,
    )
    if output_path:
        output_path.write_text(payload)
    if result.returncode != 0:
        raise RuntimeError(payload)
    data = json.loads(result.stdout)
    if not data.get("ok"):
        raise RuntimeError(result.stdout)
    return data


def text_len(xml: str) -> int:
    root = ET.fromstring("<root>" + xml + "</root>")
    text = "".join(root.itertext())
    return len(re.sub(r"\s+", "", text))


def title_text(xml: str) -> str:
    root = ET.fromstring("<root>" + xml + "</root>")
    title = root.find("title")
    return "" if title is None else "".join(title.itertext())


def main() -> None:
    EVIDENCE.mkdir(parents=True, exist_ok=True)
    stats = []
    effects = []

    for index, doc in enumerate(DOCS, 1):
        token = doc["token"]
        before = run(
            ["docs", "+fetch", "--doc", token, "--detail", "full"],
            EVIDENCE / f"{index}-before.json",
        )
        before_xml = before["data"]["document"]["content"]
        before_revision = before["data"]["document"]["revision_id"]
        content_path = ROOT / f"{index}-{token}.xml"
        content_path.write_text(doc["xml"])

        update = run(
            [
                "docs",
                "+update",
                "--doc",
                token,
                "--command",
                "block_replace",
                "--start-block-id",
                "0",
                "--end-block-id",
                "-1",
                "--content",
                "@./" + str(content_path),
                "--revision-id",
                str(before_revision),
            ],
            EVIDENCE / f"{index}-update.json",
        )
        result = update["data"].get("result")
        warnings = update["data"].get("warnings") or []
        if result != "success" or warnings:
            raise RuntimeError(f"update failed for {token}: {json.dumps(update, ensure_ascii=False)}")

        after = run(
            ["docs", "+fetch", "--doc", token, "--detail", "full"],
            EVIDENCE / f"{index}-after.json",
        )
        after_xml = after["data"]["document"]["content"]
        expected_text = "".join(ET.fromstring("<root>" + doc["xml"] + "</root>").itertext())
        actual_text = "".join(ET.fromstring("<root>" + after_xml + "</root>").itertext())
        for needle in [title_text(doc["xml"]), "结论"]:
            if needle and needle not in actual_text:
                raise RuntimeError(f"readback missing {needle!r} for {token}")
        if re.sub(r"\s+", "", expected_text) != re.sub(r"\s+", "", actual_text):
            raise RuntimeError(f"readback mismatch for {token}")

        stat = {
            "token": token,
            "title_before": title_text(before_xml),
            "title_after": title_text(after_xml),
            "revision_before": before_revision,
            "revision_after": after["data"]["document"]["revision_id"],
            "chars_before": text_len(before_xml),
            "chars_after": text_len(after_xml),
        }
        stat["reduction_chars"] = stat["chars_before"] - stat["chars_after"]
        stat["reduction_percent"] = round(stat["reduction_chars"] / stat["chars_before"] * 100, 1)
        stats.append(stat)
        effects.append(
            {
                "kind": "feishu_doc",
                "title": stat["title_after"],
                "url": f"https://bytedance.my.larkoffice.com/docx/{token}",
                "target": "Feishu Docx",
                "preview": f"原地精简 PRD Review，正文约 {stat['chars_before']} 字压缩至 {stat['chars_after']} 字。",
                "extra": json.dumps(
                    {
                        "document_id": token,
                        "revision_before": stat["revision_before"],
                        "revision_after": stat["revision_after"],
                        "chars_before": stat["chars_before"],
                        "chars_after": stat["chars_after"],
                        "reduction_percent": stat["reduction_percent"],
                    },
                    ensure_ascii=False,
                ),
            }
        )

    (ROOT / "trim-stats.json").write_text(json.dumps(stats, ensure_ascii=False, indent=2))
    (ROOT / "doc-effects.json").write_text(json.dumps(effects, ensure_ascii=False, indent=2))
    print(json.dumps({"stats": stats, "effects": effects}, ensure_ascii=False, indent=2))


if __name__ == "__main__":
    main()
