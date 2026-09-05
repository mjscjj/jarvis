# Jarvis 世界模型与维护逻辑 Slides

一套基于当前 Jarvis 代码与《主动式数字分身》第 4 节制作的 16 页工程白板风演示。内容覆盖世界模型本体、FactEngine 维护循环、工程可靠性边界，以及 M3/M5 如何渐进消费世界认知。

## 本地播放

```bash
npm install --no-audit --no-fund
npm run dev
```

打开 `http://127.0.0.1:4186/`。支持方向键、空格、空白点击、触控滑动和右侧页码轮；实体与认知卡片可独立交互。

稳定帧地址：`?scene=<scene-id>&beat=<beat>&snapshot=1`。例如：

```text
http://127.0.0.1:4186/?scene=compile-loop&beat=4&snapshot=1
```

## 构建与 PDF

```bash
npm run build
npm run preview
DECK_URL=http://127.0.0.1:4187 npm run export:pdf
```

输出：

- 静态 HTML：`dist/`
- 16 页 PDF：`jarvis-world-model-maintenance.pdf`

## 验证

```bash
npm test
```

测试覆盖 16 个稳定场景的所有 beat、Chromium/WebKit、错误路由、键盘/点击/滑动、局部交互隔离、移动端缩放、本地字体、打印帧、溢出检查与代表页视觉基线。
