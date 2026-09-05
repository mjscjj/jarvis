# 验证记录

## Fireworks 结构与几何门

执行：

```bash
bash /Users/bytedance/.codex/skills/fireworks-tech-graph/scripts/validate-svg.sh \
  docs/summery/architecture-skill-comparison/fireworks/jarvis-reality-loop.svg
```

结构检查结果：

- XML structure：通过
- marker references：通过
- arrow collisions：通过
- semantic geometry：通过
- composition quality：通过

Skill 包装脚本的最后一项 CairoSVG render validation 未执行成功：当前机器没有 native `libcairo`，临时安装 Python `cairosvg` 后仍无法加载该动态库。没有把此环境缺失伪装成通过。

## PNG 渲染替代路径

依据 Skill 的 `references/png-export.md`，改用 Playwright + Chrome 浏览器渲染：

```bash
NODE_PATH=/Users/bytedance/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules \
  /Users/bytedance/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node -e '<Playwright browser export>'
```

结果：

- PNG：`jarvis-reality-loop.png`
- 像素：3200 × 2240
- SVG 和 PNG 均保留在本目录，未覆盖任何既有架构图。

## 视觉回看

`visual_review: passed`

已实际加载最终 PNG 检查：

- 主链从外部现实到真实结果可连续阅读；
- M3 / M5 职责边界可辨认；
- 红色验证失败回路和绿色世界状态反馈回路均可见；
- 未发现文字截断、文字互相覆盖、连线穿节点或画布裁切；
- 图例、事实源声明与 runtime 边界不占用业务流走廊。

## 已知限制

- 静态图无法按步骤展开 M5 循环；需要交互讲解时应使用同组的 Interactive Architecture Diagrams 版本。
- 信息密度针对桌面/文档大图优化；在手机宽度下应先查看 PNG 全图，再放大局部。
