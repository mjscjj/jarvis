# Jarvis 主动式数字分身 Slides

基于《主动式数字分身探索实践：世界模型与决策方案》和 Jarvis 当前代码制作的完整 20 页互动 Slides。

```bash
npm install --no-audit --no-fund
npm run dev
```

本地预览：`http://127.0.0.1:4188/`

完整总入口：`http://127.0.0.1:4188/`

核心页面稳定入口：

- 第 10 页：`http://127.0.0.1:4188/?scene=system-core&beat=3`
- 第 12 页：`http://127.0.0.1:4188/?scene=world-anatomy&beat=3`
- 第 15 页：`http://127.0.0.1:4188/?scene=decision-pipeline&beat=4`

1–20 页最终帧静态预览位于 `docs/previews/`。世界模型页中的 Summary / Fact / PageRevision 可以点击切换。

验证：

```bash
npm run build
npm test
npm run export:pdf
```

PDF：`artifacts/jarvis-active-digital-twin-20p.pdf`
