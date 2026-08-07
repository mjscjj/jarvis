import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Empty, Flex, Input, Spin, Tabs, Tag, Typography } from 'antd'
import {
  getAgentConfigPreview,
  listTextFiles,
  listWorkRules,
  updateTextFile,
  updateWorkRule,
} from './api'
import PageHeader from './components/PageHeader'
import { usePageContext } from './pageContext'
import type { AgentConfigPreview, AgentConfigStage, TextFile, WorkRule } from './types'
import './styles/agent-settings.css'

const { Text } = Typography

type AgentSettingsView = AgentConfigStage | 'other'
type StageSection = 'prompt' | 'rules' | 'approval' | 'preview'

const dynamicBlockLabels: Record<string, string> = {
  principal_open_id: 'Principal 身份',
  tool_catalog: '工具目录',
  shared_memory: '共享记忆',
  skills: 'Skills',
  conversation_context: '会话上下文',
  output_contract: '输出协议',
  phase_instructions: '执行阶段指令',
  task_context: 'Task 上下文',
  output_schema: '输出 Schema',
}

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

interface EditorProps {
  title: string
  description: string
  path?: string
  value: string
  placeholder?: string
  saving: boolean
  allowEmpty?: boolean
  onChange: (value: string) => void
  onSave: () => void
}

function MarkdownEditor({
  title,
  description,
  path,
  value,
  placeholder,
  saving,
  allowEmpty = false,
  onChange,
  onSave,
}: EditorProps) {
  return (
    <Card className="agent-config-card" variant="borderless">
      <div className="agent-config-card-heading">
        <div>
          <Text strong>{title}</Text>
          <div><Text type="secondary">{description}</Text></div>
        </div>
        {path && <Text code>{path}</Text>}
      </div>
      <Input.TextArea
        value={value}
        onChange={(event) => onChange(event.target.value)}
        autoSize={{ minRows: 12, maxRows: 28 }}
        placeholder={placeholder}
        className="agent-markdown-editor"
      />
      <Flex justify="flex-end" style={{ marginTop: 12 }}>
        <Button type="primary" onClick={onSave} loading={saving} disabled={!allowEmpty && !value.trim()}>
          保存修改
        </Button>
      </Flex>
    </Card>
  )
}

function EffectivePreview({ preview }: { preview?: AgentConfigPreview }) {
  if (!preview) return <Card variant="borderless"><Empty description="暂无生效预览" /></Card>
  return (
    <Card className="agent-config-card agent-preview-card" variant="borderless">
      <div className="agent-config-card-heading">
        <div>
          <Text strong>配置生效预览</Text>
          <div><Text type="secondary">已展开工作规则和审批规则；下列运行时内容会在真实执行时继续注入。</Text></div>
        </div>
      </div>
      <Flex gap={6} wrap className="agent-dynamic-blocks">
        {preview.dynamic_blocks.map((block) => <Tag key={block}>{dynamicBlockLabels[block] ?? block}</Tag>)}
      </Flex>
      <Input.TextArea
        value={preview.content}
        readOnly
        autoSize={{ minRows: 18, maxRows: 34 }}
        className="agent-markdown-editor agent-preview-editor"
      />
    </Card>
  )
}

export default function AgentSettings() {
  const { context, setViewState } = usePageContext()
  const requestedView = context.view_state.stage
  const activeView: AgentSettingsView = requestedView === 'm3' || requestedView === 'other' ? requestedView : 'm5'
  const [textFiles, setTextFiles] = useState<TextFile[]>([])
  const [workRules, setWorkRules] = useState<WorkRule[]>([])
  const [textDrafts, setTextDrafts] = useState<Record<string, string>>({})
  const [ruleDrafts, setRuleDrafts] = useState<Partial<Record<WorkRule['key'], string>>>({})
  const [previews, setPreviews] = useState<Partial<Record<AgentConfigStage, AgentConfigPreview>>>({})
  const [loading, setLoading] = useState(false)
  const [savingKey, setSavingKey] = useState<string>()
  const [error, setError] = useState<string>()
  const [notice, setNotice] = useState<string>()
  const [otherKey, setOtherKey] = useState<string>()
  const [stageSections, setStageSections] = useState<Record<AgentConfigStage, StageSection>>({
    m3: 'prompt',
    m5: 'prompt',
  })

  const reloadPreviews = useCallback(async () => {
    const [m3, m5] = await Promise.all([getAgentConfigPreview('m3'), getAgentConfigPreview('m5')])
    setPreviews({ m3, m5 })
  }, [])

  const reload = useCallback(() => {
    setLoading(true)
    Promise.all([listTextFiles(), listWorkRules(), getAgentConfigPreview('m3'), getAgentConfigPreview('m5')])
      .then(([fileResult, ruleResult, m3, m5]) => {
        setTextFiles(fileResult.items)
        setWorkRules(ruleResult.items)
        setTextDrafts(Object.fromEntries(fileResult.items.map((item) => [item.key, item.content])))
        setRuleDrafts(Object.fromEntries(ruleResult.items.map((item) => [item.key, item.content])))
        setPreviews({ m3, m5 })
        const others = fileResult.items.filter((item) => item.stage !== 'm3' && item.stage !== 'm5')
        setOtherKey((current) => current && others.some((item) => item.key === current) ? current : others[0]?.key)
        setError(undefined)
      })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])

  useEffect(reload, [reload])

  const filesByKey = useMemo(() => Object.fromEntries(textFiles.map((item) => [item.key, item])), [textFiles])
  const rulesByKey = useMemo(
    () => Object.fromEntries(workRules.map((item) => [item.key, item])) as Partial<Record<WorkRule['key'], WorkRule>>,
    [workRules],
  )
  const otherFiles = useMemo(() => textFiles.filter((item) => item.stage !== 'm3' && item.stage !== 'm5'), [textFiles])

  const saveText = async (key: string) => {
    const content = textDrafts[key] ?? ''
    if (!content.trim()) {
      setError(`${filesByKey[key]?.name ?? key}不能为空`)
      return
    }
    setSavingKey(`text:${key}`)
    try {
      const updated = await updateTextFile(key, { content })
      setTextFiles((current) => current.map((item) => item.key === key ? updated : item))
      setTextDrafts((current) => ({ ...current, [key]: updated.content }))
      await reloadPreviews()
      setNotice(`${updated.name}已保存，后续运行会实时读取`)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingKey(undefined)
    }
  }

  const saveRule = async (key: WorkRule['key']) => {
    setSavingKey(`rule:${key}`)
    try {
      const updated = await updateWorkRule(key, { content: ruleDrafts[key] ?? '' })
      setWorkRules((current) => current.map((item) => item.key === key ? updated : item))
      setRuleDrafts((current) => ({ ...current, [key]: updated.content }))
      await reloadPreviews()
      setNotice(`${updated.name}工作规则已保存，后续运行会实时读取`)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingKey(undefined)
    }
  }

  const stagePanel = (stage: AgentConfigStage) => {
    const isM3 = stage === 'm3'
    const stageName = isM3 ? '线索发现' : '任务执行'
    const promptKey = isM3 ? 'm3_system_prompt' : 'm5_system_prompt'
    const stageRuleKey: WorkRule['key'] = isM3 ? 'extract' : 'execute'
    const promptFile = filesByKey[promptKey]
    const stageRule = rulesByKey[stageRuleKey]
    const approvalFile = filesByKey.m5_approval_policy
    const sections = [
      {
        key: 'prompt',
        label: '系统提示词',
        children: promptFile ? (
          <MarkdownEditor
            title={promptFile.name}
            description={promptFile.description}
            path={promptFile.path}
            value={textDrafts[promptFile.key] ?? ''}
            saving={savingKey === `text:${promptFile.key}`}
            onChange={(value) => setTextDrafts((current) => ({ ...current, [promptFile.key]: value }))}
            onSave={() => saveText(promptFile.key)}
          />
        ) : <Empty description="系统提示词未加载" />,
      },
      {
        key: 'rules',
        label: '工作规则',
        children: stageRule ? (
          <MarkdownEditor
            title={`${stageName}工作规则`}
            description={`只在${stageName}阶段注入；不会与其他 Agent 共享。`}
            path={stageRule.path}
            value={ruleDrafts[stageRuleKey] ?? ''}
            saving={savingKey === `rule:${stageRuleKey}`}
            allowEmpty
            onChange={(value) => setRuleDrafts((current) => ({ ...current, [stageRuleKey]: value }))}
            onSave={() => saveRule(stageRuleKey)}
          />
        ) : <Empty description="工作规则未加载" />,
      },
      ...(!isM3 ? [{
        key: 'approval',
        label: '审批策略',
        children: approvalFile ? (
          <MarkdownEditor
            title={approvalFile.name}
            description={approvalFile.description}
            path={approvalFile.path}
            value={textDrafts[approvalFile.key] ?? ''}
            saving={savingKey === `text:${approvalFile.key}`}
            onChange={(value) => setTextDrafts((current) => ({ ...current, [approvalFile.key]: value }))}
            onSave={() => saveText(approvalFile.key)}
          />
        ) : <Empty description="审批策略未加载" />,
      }] : []),
      {
        key: 'preview',
        label: '生效预览',
        children: <EffectivePreview preview={previews[stage]} />,
      },
    ]
    return (
      <div className="agent-stage-content">
        <Text type="secondary" className="agent-stage-hint">
          {`${stageName}使用真实运行时模板。${isM3
            ? '系统提示词必须保留一个 {{WORK_RULES}}；保存时会严格校验，运行时在该位置展开线索发现工作规则。'
            : '模板必须各保留一个 {{WORK_RULES}} 和 {{APPROVAL_POLICY}}；execute、apply 和 Session 恢复使用同一套组装逻辑。'}`}
        </Text>
        <Card className="agent-config-card agent-stage-tabs-card" variant="borderless">
          <Tabs
            className="agent-stage-tabs"
            activeKey={stageSections[stage]}
            onChange={(key) => setStageSections((current) => ({ ...current, [stage]: key as StageSection }))}
            items={sections}
          />
        </Card>
      </div>
    )
  }

  return (
    <div className="agent-settings-page">
      <PageHeader title="Agent 设置" subtitle="配置任务执行、线索发现及其他 Agent" />
      {error && <Alert type="error" showIcon title="Agent 设置操作失败" description={error} closable onClose={() => setError(undefined)} />}
      {notice && <Alert type="success" showIcon title={notice} closable onClose={() => setNotice(undefined)} />}
      <Spin spinning={loading}>
        <Tabs
          activeKey={activeView}
          onChange={(stage) => setViewState({ stage })}
          items={[
            { key: 'm5', label: '任务执行', children: stagePanel('m5') },
            { key: 'm3', label: '线索发现', children: stagePanel('m3') },
            {
              key: 'other',
              label: '其他 Agent',
              children: otherFiles.length ? (
                <Card className="agent-config-card" variant="borderless">
                  <Tabs
                    tabPosition="left"
                    activeKey={otherKey}
                    onChange={setOtherKey}
                    items={otherFiles.map((item) => ({
                      key: item.key,
                      label: item.name,
                      children: (
                        <MarkdownEditor
                          title={item.name}
                          description={item.description}
                          path={item.path}
                          value={textDrafts[item.key] ?? ''}
                          saving={savingKey === `text:${item.key}`}
                          onChange={(value) => setTextDrafts((current) => ({ ...current, [item.key]: value }))}
                          onSave={() => saveText(item.key)}
                        />
                      ),
                    }))}
                  />
                </Card>
              ) : <Empty description="没有其他 Agent 提示词" />,
            },
          ]}
        />
      </Spin>
    </div>
  )
}
