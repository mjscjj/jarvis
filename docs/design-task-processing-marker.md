# 处理中标记的收尾（OnIt reaction 撤回）

> Status: proposal
> Authority: non-normative
> Last verified: 2026-09-03 @ `034ee94`

未实现。本文只记录方案与待定项，不描述当前代码行为。

## 问题

M5 开始处理一个有飞书源消息的 Task 时，runtime 会给源消息加一个 `OnIt` reaction 作为 best-effort 开始确认（`internal/execute/agent_executor.go` 的 `startTaskFeedback`，实际调用在 `internal/taskfeedback/notifier.go`）。这个标记加上之后永不撤回。

后果是无论 M5 最终 `completed`、`observing` 还是 `failed`，飞书侧都只留一个「正在处理」的标记，从会话里完全看不出结论。

真实案例：Task 1211（Todo 2751，吴邹杰在 Bax (For Ops)- 问题反馈群报 PPE/Prod 数据不通）。M5 在 2026-09-03 10:19:43–10:22:01 跑完 run 1463，核验到 Pulse 已建 issue `999998731209` 并分配给 何炫燃，判断不需要重复对外动作，返回 `observing`。群里只剩那个 `OnIt`，于是被误读成「Jarvis 卡住了 / 没执行」。

## 为什么不交给 M5 自己撤

技术上 M5 能做：它是 `danger-full-access`，`lark-cli im reactions delete --message-id ... --reaction-id ...` 存在，且飞书只允许删除自己添加的 reaction，而这个 reaction 正是 Bot 加的。

但不应该这样做：

1. **拿不到 `reaction_id`。** 它只写在 `execution_run.effects`；注入 M5 prompt 的历史轮次只带 summary / output / error_detail（`internal/execute/prior_runs.go` 的 `runViewToPriorSummary`），不带 effects。当前轮的 effect 更是在 run 刚起步时才落库，模型看不到。要让模型撤，得先把 id 注入 prompt，或让它 `im reactions list` 反查再删。
2. **语义所有权不在模型。** `conf/prompts/m5-system-prompt.md:107` 已经把这个 reaction 定义成 runtime 的 best-effort 开始确认，不是 M5 的业务动作。谁贴谁收。
3. **「还算不算正在处理」不是语义判断**，它就是执行状态本身，属于机器硬边界。交给模型判断只多一种漏法：忘了就永久残留。

## 方案

在 runtime 侧补上对称的撤回，M5 的 prompt、rules、工具目录都不改，模型不需要知道这件事。

### 1. `internal/taskfeedback/notifier.go`

加与 `AddProcessingReaction` 对称的删除方法：

```go
func (n *Notifier) RemoveProcessingReaction(ctx context.Context, messageID, reactionID string) error {
	messageID = strings.TrimSpace(messageID)
	reactionID = strings.TrimSpace(reactionID)
	if messageID == "" || reactionID == "" {
		return fmt.Errorf("Task feedback reaction target is invalid message_id=%q reaction_id=%q", messageID, reactionID)
	}
	params, err := json.Marshal(map[string]string{"message_id": messageID, "reaction_id": reactionID})
	if err != nil {
		return fmt.Errorf("encode Task processing reaction delete params: %w", err)
	}
	if err := n.lark.Run(ctx, nil,
		"im", "reactions", "delete",
		"--params", string(params), "--as", "bot",
	); err != nil {
		return fmt.Errorf("remove Task processing reaction reaction_id=%s: %w", reactionID, err)
	}
	return nil
}
```

包头注释需同步更新：现在写的是 `owns only the best-effort OnIt start acknowledgement`，加了撤回后语义变成「拥有处理中标记的完整生命周期」。不改的话下一个人会以为撤回逻辑放错了地方。

### 2. `internal/execute/agent_executor.go`

`TaskFeedbackNotifier` 接口加一个方法：

```go
type TaskFeedbackNotifier interface {
	AddProcessingReaction(context.Context, TaskFeedbackTarget) (*TaskFeedbackReaction, error)
	RemoveProcessingReaction(ctx context.Context, messageID, reactionID string) error
}
```

在 `startTaskFeedback` 旁边加反向操作。它把 `startTaskFeedback` 写进 `run.Effects` 的那条记录读回来，删表情，再按 AGENTS.md §5 补一条 `operation: "remove"` 留痕：

```go
// clearTaskFeedback removes the OnIt start acknowledgement once this run stops
// processing the Task. Symmetric to startTaskFeedback and equally best-effort:
// a stale marker is never a reason to fail a finished run.
func (e *AgentExecutor) clearTaskFeedback(ctx context.Context, run *domain.ExecutionRun) {
	if e.feedback == nil || run == nil || len(run.Effects) == 0 {
		return
	}
	var effects []map[string]any
	if err := json.Unmarshal(run.Effects, &effects); err != nil {
		hlog.CtxWarnf(ctx, "decode effects while clearing Task processing reaction run_id=%d error=%+v", run.ID, err)
		return
	}
	for _, effect := range effects {
		if effect["purpose"] != "task_processing" || effect["operation"] != "add" {
			continue
		}
		messageID, _ := effect["source_message_id"].(string)
		reactionID, _ := effect["reaction_id"].(string)
		if err := e.feedback.RemoveProcessingReaction(ctx, messageID, reactionID); err != nil {
			hlog.CtxWarnf(ctx, "Task processing reaction not removed run_id=%d reaction_id=%s error=%+v", run.ID, reactionID, err)
			continue
		}
		updated, err := appendTaskFeedbackEffect(run.Effects, map[string]any{
			"kind": "feishu_reaction", "title": "M5 处理结束", "purpose": "task_processing",
			"reaction_id": reactionID, "source_message_id": messageID,
			"emoji_type": "OnIt", "operation": "remove",
		})
		if err != nil {
			hlog.CtxWarnf(ctx, "record Task processing reaction removal run_id=%d error=%+v", run.ID, err)
			return
		}
		run.Effects = datatypes.JSON(updated)
		if err := e.persistRun(ctx, run); err != nil {
			hlog.CtxWarnf(ctx, "save Task processing reaction removal run_id=%d error=%+v", run.ID, err)
		}
		return
	}
}
```

### 3. 唯一调用点：`finishRun` 末尾

放在 `store.Finish` 之后、构造 `ExecuteResult` 之前，**不加任何 outcome 判断**：

```go
	}); err != nil {
		return nil, fmt.Errorf("finish Task id=%d after execution: %w", task.ID, err)
	}
	e.clearTaskFeedback(ctx, run)
	result := &ExecuteResult{
```

一行就够，是因为「还会回来」的三种状态都不经过这里：`waiting` 和 `needs_human` 在 `finishRun` 内部提前 return，`awaiting_approval` 更早在 `routeRun` 里 return。能走到这一行的只有 `done` / `observing` / `failed`，天然等价于「本轮真的收工了」。因此不需要写 `if outcome == "observing"` 这类按结果分流的代码。

### 4. 测试

`internal/execute/task_feedback_test.go`：

- `observing` 收尾调用一次 `RemoveProcessingReaction`，并在 effects 落一条 `operation: "remove"`。
- `waiting` 收尾一次都不调（锁住上面那条控制流契约）。

## 风险与待定项

**隐式控制流契约。** `clearTaskFeedback` 的正确性依赖「`finishRun` 上游几段都是 return」这个事实，不受编译器或类型保护。将来在 `finishRun` 底部新增一个「其实还会继续」的收尾状态，表情会被错撤且不报错。缓解只能靠调用点注释写明能走到这里的状态集合，加上 `waiting` 那条测试。这是本方案换取「不按 outcome 分流」的代价，接受但需显式记录。

**跨轮残留，未验证。** `waiting` 续跑会再次 `startTaskFeedback`（`agent_executor.go` 中 472 / 1113 / 1229 三处），而撤回只处理当前 run 记录的 id。若飞书对（消息, emoji, Bot）唯一、重复 add 返回同一 `reaction_id`，则无残留；若每次返回新 id，多轮 Task 会堆积表情。**实施前必须实测一个 `waiting` → 续跑 → 终态的真实 Task。** 若确认会堆积，改为撤回时遍历该 Task 所有 run 的 `task_processing` effect 逐个删除（约多十行）。

## 未决策：撤干净还是留结论

撤掉之后，源消息上不留任何痕迹，外部依然看不出 Jarvis 看过并判断过。原始困惑其实是两件事：

- 「看起来卡住」——撤回解决。
- 「看不出结论」——撤回反而让它更彻底地看不出来。

若要后者，方向是终态换表情而非清空（如 `completed` 换 `Done`、`observing` 换 `Eyes`），代价是需要一处「结果 → emoji」映射，重新引入按 outcome 分流的写法，写法与上文不同。

**待 principal 决定：处理完之后源消息上不留痕迹，还是留一个能看出结论的痕迹。** 本文按前者写。
