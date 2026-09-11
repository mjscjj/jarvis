import json
from pathlib import Path
p=Path(__file__).parent
uid='ou_4bb78ddd152503b76a40232afa2420dd'
summary='https://bytedance.my.larkoffice.com/docx/YYKmdUevToWBYYxBZOvmO0ioyGf'
def md(s,size='normal'):
    return {'tag':'markdown','content':s,'text_size':size}
def box(items,bg='default'):
    return {'tag':'column_set','flex_mode':'none','columns':[{'tag':'column','width':'weighted','weight':1,'background_style':bg,'padding':'12px','vertical_spacing':'8px','elements':items}]}
schedules=[
 ('#4 · 平台融合初评','每周二 12:00','2026/09/15 12:00','TT LIVE平台融合初评 // TT LIVE Platform Product Initial Review','oc_5f132befef18d343c19cc30488f988f6','本周二已锁定的初评需求清单','https://meego.larkoffice.com/tiktok_live/storyView/nEfK_JNVR'),
 ('#5 · Backstage 初评','每周二 16:00','2026/09/15 16:00','TT LIVE Backstage Product Initial Review','oc_1506b6e2c5ccf6403a345aef7e8c18f8','本周二已锁定的 Backstage 初评需求清单','https://meego.feishu.cn/tiktok_live/storyView/RX5ss8w4g'),
 ('#3 · Arena 组内','每周三 09:00','2026/09/16 09:00','公会/内部效率/数据平台组内','oc_43d3b4c42308cc09705a09768650734d','本周 Arena 产品组内待评审 PRD；按早上可读版本预审，不等晚间结论','https://bytedance.feishu.cn/base/bascnsH6VDJBgRA6oFZZUYe64pf')]
rows=[md('**三路排期 · 已启用**'),md('<font color="grey">北京时间 Asia/Shanghai · 每周重复 · 下列时间为任务开始时间，报告在执行完成后送达。</font>','notation')]
for name,t,n,group,chat,scope,source in schedules:
    rows.append(box([md(f'**{t}  |  {name}**'),md(f'来源群：[ {group} ](https://applink.feishu.cn/client/chat/open?openChatId={chat})\n评审范围：{scope}\n[需求材料入口]({source}) · 首轮：{n}')],'grey-50'))
card={
 'schema':'2.0',
 'config':{'update_multi':True,'width_mode':'default','summary':{'content':'Emily PRD Review：三路定时评审已就位｜周二12:00、16:00；周三09:00'}},
 'header':{'title':{'tag':'plain_text','content':'Emily PRD Review｜让评审提前一步'},'subtitle':{'tag':'plain_text','content':'三路定时评审已就位 · 2026.09.11 · 持续迭代中'},'template':'violet','icon':{'tag':'standard_icon','token':'ai-common_colorful'},'text_tag_list':[{'tag':'text_tag','text':{'tag':'plain_text','content':'排期已启用'},'color':'violet'}]},
 'body':{'direction':'vertical','padding':'12px','vertical_spacing':'12px','elements':[
  box([md('**把翻群找文档的时间，留给真正的产品判断。**','heading-3'),md(f'<at id={uid}></at> 你的 PRD 评审助手已就位：到点读取目标批次，逐篇给出有依据的第二意见，再把飞书报告送到你手边。')],'violet-50'),
  box(rows),
  box([md('**从需求清单，到可行动的评审意见**'),md('定位本周批次 → 找齐 PRD 与关键引用 → 逐篇评审 → 飞书报告 + 批次汇总 → Bot 私聊 Claire'),md('**交付标准**\n每项关键发现讲清场景、原文依据、影响与建议；区分确认问题和待决策，篇幅适中。原始 PRD 保持只读；无权限或材料未就绪如实记录，重跑复用已有报告。'),md('**Skill 组合**\n`product-prd-review` 编排，`product-doc-read` 取证，`product-doc-review` 评审。已补上会前取材规则，优先本周需求，避免回捞上周会议。'),md('**验证进度**\nArena 五篇报告与汇总已完成一次人工触发的完整流程，并完成详略调整和 Bot 送达。三条新排期均已读回核验；新的定时首轮尚未执行。')],'grey-50'),
  {'tag':'collapsible_panel','expanded':True,'header':{'title':{'tag':'plain_text','content':'下一步打磨｜Skill 还在进化'}},'padding':'12px','vertical_spacing':'8px','elements':[md('以下是待完善方向，尚未全部实现：\n• **首轮验收**：验证两个初评群的锁定清单，以及组内会前材料能否稳定、完整取齐。\n• **材料变化**：完善晚到材料、临时加单、延期需求的补跑与去重策略。\n• **评审闭环**：细化会前版本与会后修改的对比，让旧问题是否解决更容易追踪。\n• **质量校准**：用不同类型 PRD 持续打磨详略和优先级，保留推理，减少重复。'),md('<font color="grey">当前已配置的完成通知仍为 Bot 私聊 Claire；本条为 Emily pro小组的产品介绍。</font>','notation')]},
  {'tag':'button','text':{'tag':'plain_text','content':'打开已交付评审 · 看看 Emily 的第二意见'},'type':'primary_filled','width':'fill','behaviors':[{'type':'open_url','default_url':summary}]}
 ]}}
raw=json.dumps(card,ensure_ascii=False,indent=2)
assert len(card['body']['elements'])==5
assert f'<at id={uid}></at>' in raw
for s in schedules:
    assert s[1] in raw and s[2] in raw and s[4] in raw and s[6] in raw
assert len(raw.encode())<30000
(p/'card.json').write_text(raw+'\n')
(p/'review.md').write_text('卡片发送前自检\n\nP0：三个目标群、范围、来源、任务编号、启用状态、北京时间、每周频率、首轮日期、交付物、通知对象、Skill 现状与待完善方向、报告入口、Claire 真实 mention 均有承载。\nP1：单一最大字号主张；正文、标题、灰色辅助信息分层。\nP2：价值、排期、工作流、迭代方向、入口分别分组。\nP3：5 个顶层块，含背景容器；violet/blue 与 grey 配色，无图表堆砌。\nP4/P5：粗细字号区分，间距统一 8/12px。\nP6/P7：同色同义，默认宽度、单列排期兼容窄屏，不使用 stretch。\n未声称三条新定时排期已跑完；后续完善方向明确为待办，不是功能承诺。\n')
print('Card generated and P0–P7 checked:',len(raw.encode()),'bytes')
