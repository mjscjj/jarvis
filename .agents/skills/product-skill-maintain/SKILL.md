---
name: product-skill-maintain
description: 根据用户反馈和实际 Task 执行证据维护本项目产品管理 Skills，检查绑定、改写流程说明并验证改动。适用于明确的产品 Skill 优化或维护任务。
---

# 产品 Skills 维护

以当前 Task 指定的优化目标和失败/成功案例为依据，读取对应 Task 的输入、执行过程、结果及当前 Skill 文件。没有具体效果证据时先调查，不靠扩写提示词假装优化。普通产品阅读或 Review 不自动触发 Skill 改写。

正文唯一真源是 .agents/skills/<name>/SKILL.md；conf/skills.yaml 保存阶段与启用配置，internal/plugin/catalog.go 的 Manifest 保存插件所属 Skill 名称。产品管理 Skills 以 execute 目录方式按需读取，不把业务正文注入 M3 或通用系统提示词。用户页面用于观察，Agent 使用文件与通用工具维护。

修改前读 AGENTS.md，检查工作区和目标文件最新内容。保持稳定名称、调用范围、外部副作用边界和已有用户约束。只将可复用的方法写进 Skill，具体项目事实留在任务背景或世界模型。不要将正文复制到数据库或定时任务 instruction。

修改后检查 frontmatter、引用文件、阶段配置和插件绑定一致，结合原案例验证改动能解决实际问题；需要外发或改线上业务数据的验证不作为默认测试。保留 diff、验证依据和剩余限制到原 Task。遇到并行修改先重新读取并合并，不覆盖他人改动。

新增或重命名 Skill 涉及注册时同步维护绑定和测试；仅改正文由下一次读取生效，运行中 Session 已读入的内容不会自动替换。不要自行创建循环优化计划、变更无关 Skills、提交或部署未授权的代码。
