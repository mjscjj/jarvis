# Jarvis：主动式任务数字分身

18 页、1920×1080 的本地 HTML 演示，配套一份 reading-first 技术产品方案稿。

## 使用

```bash
npm install
npm run dev
```

浏览器打开 `http://127.0.0.1:4176`。使用方向键、空格、点击空白区域或滑动翻页；右侧圆点可以直接跳转场景。

稳定画面 URL：

```text
?scene=pipeline&beat=3&snapshot=1
```

## 验证与交付

```bash
npm test
npm run build
npm run export:pdf
```

- HTML 静态产物：`dist/`
- PDF：`artifacts/jarvis-proactive-digital-twin.pdf`
- 完整渲染截图：`artifacts/rendered/`
- 方案稿：`proposal.md`
- 决策与证据：`docs/context.md`
