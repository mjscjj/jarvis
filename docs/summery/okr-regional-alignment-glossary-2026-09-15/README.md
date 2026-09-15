# 区域 OKR 英文 glossary 读取记录

读取时间：2026-09-15（UTC）

## 用户指定来源

- <https://bytedance.my.larkoffice.com/sheets/DgBcsRc49hwN7ltuCDllS2gwgAe>
- <https://bytedance.sg.larkoffice.com/sheets/Xxl9s00jYh8nn9tqfZBlSXVGg8g>
- <https://bytedance.sg.larkoffice.com/sheets/RVpQsK3mwh38Zyt2Pexl0LJng7b>

## 读取结果

用户补充查看权限后，使用开发实例中已存在且包含 `sheets:spreadsheet:read` 的网页登录授权完成只读查询。已读取：

- `DgBcs...` Sheet1：113 个有效行（含表头），提取 111 条术语。
- `Xxl9...` Terminology：82 个有效内容行，提取 81 条术语；优先采用 `@周旻淅 Minxi Zhou Final EN`，为空时使用 `英文 / EN`。
- `Xxl9...` Terminology-backup：171 个有效行（含表头），提取 169 条术语，作为最低优先级补充。
- `RVpQ...` Sheet1：110 个有效行（含表头），提取 107 条 Arena/UI 术语。

合并时按 `Terminology Final EN → business Sheet1 → Arena/UI → Terminology-backup` 处理重复项，最终得到 355 个唯一中文术语，保存到 `data/okr/translation-glossary.json`。模型每批只接收在当前原文中实际出现的 glossary 条目，并优先使用最长匹配，避免较短词覆盖复合术语。

原始 Sheets API 回执及各工作表单元格值位于 `evidence/`。

## 应用与核验

- glossary 内容哈希已纳入翻译缓存版本；术语表变更后，旧英文不会被继续复用。
- 已重新翻译区域对齐页当前 Plan/Review 的 829 个唯一文本，数据库中 829 行均使用同一新 glossary 哈希。
- 621 个文本命中至少一个 glossary 条目，共 1451 个术语出现位置。
- 对 Final EN 与业务 Sheet1 两个高优先级来源做大小写无关、支持 `/` 备选词的严格检查：676/676 个出现位置采用指定英文，未命中 0。
- 页面 API 回读：Plan 19 个 Objective；上季度复盘为 `2026-W36`，25 个 Objective、288 条具体 KR 进展；翻译映射 829 条。
