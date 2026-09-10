import { useCallback, useEffect, useState } from 'react'
import { listTextFiles, updateTextFile } from '../../../api'
import { usePageContext } from '../../../pageContext'
import type { TextFile } from '../../../types'
import type { OKRActionDefinition } from '../actionConfig'
import { AgentActionCenter } from './AgentActionCenter'

function errorText(cause: unknown) {
  return cause instanceof Error ? cause.message : String(cause)
}

export function AgentFlowCenter({ weeklyEnabled, quarter }: { weeklyEnabled: boolean; quarter: string }) {
  const { context, setViewState } = usePageContext()
  const [items, setItems] = useState<TextFile[]>([])
  const [loading, setLoading] = useState(true)
  const [notice, setNotice] = useState<{ kind: 'error'; text: string }>()

  const load = useCallback(async (signal?: AbortSignal) => {
    setLoading(true)
    try {
      const response = await listTextFiles(signal)
      setItems(response.items.filter((item) => item.stage === 'okr_agent'))
      setNotice(undefined)
    } catch (cause) {
      if (!signal?.aborted) setNotice({ kind: 'error', text: `Agent Prompt 读取失败：${errorText(cause)}` })
    } finally {
      if (!signal?.aborted) setLoading(false)
    }
  }, [])

  useEffect(() => {
    const controller = new AbortController()
    void load(controller.signal)
    return () => controller.abort()
  }, [load])

  useEffect(() => {
    if (!quarter || context.view_state.quarter === quarter) return
    setViewState({ ...context.view_state, tab: 'agent-flows', quarter }, true)
  }, [context.view_state, quarter, setViewState])

  const openAction = (action: OKRActionDefinition) => {
    setViewState({
      ...context.view_state,
      tab: 'agent-flows',
      prompt_key: action.promptKey,
      action_key: action.key,
      action_label: action.title,
    }, true)
  }

  const openPrompt = (prompt: TextFile) => {
    setViewState({
      ...context.view_state,
      tab: 'agent-flows',
      prompt_key: prompt.key,
      action_key: undefined,
      action_label: prompt.name,
    }, true)
  }

  const savePrompt = async (key: string, content: string) => {
    const updated = await updateTextFile(key, { content })
    setItems((current) => current.map((item) => item.key === updated.key ? updated : item))
    return updated
  }

  return (
    <div className="space-y-3">
      {notice && <div className="rounded-lg border border-red-100 bg-red-50 px-3 py-2 text-xs text-red-700">{notice.text}</div>}

      <AgentActionCenter
        prompts={items}
        promptsLoading={loading}
        actionsEnabled={weeklyEnabled}
        initialPromptKey={context.view_state.prompt_key}
        onReloadPrompts={() => void load()}
        onSavePrompt={savePrompt}
        onOpenAction={openAction}
        onOpenPrompt={openPrompt}
      />
    </div>
  )
}
