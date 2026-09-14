import { useCallback, useEffect, useMemo, useState } from 'react'
import { Alert, Button, Card, Empty, Flex, Input, Radio, Spin, Tabs, Tag, Typography } from 'antd'
import {
  getAgentConfigPreview,
  listTextFiles,
  listWorkRules,
  updateTextFile,
  updateWorkRule,
} from './api'
import PageHeader from './components/PageHeader'
import { usePageContext } from './pageContext'
import type { AgentConfigPreview, AgentConfigStage, InitiativeLevel, TextFile, WorkRule } from './types'
import './styles/agent-settings.css'

const { Text } = Typography

type RuleStage = 'm3' | 'm5'
type AgentSettingsView = RuleStage | 'other'
type StageSection = 'prompt' | 'rules' | 'approval' | 'preview'

const initiativeKey = 'initiative_level'
const initiativeLevels: { value: InitiativeLevel, label: string, description: string }[] = [
  { value: 'quiet', label: '安静', description: '优先处理明确交办和确定需要介入的事；未获授权的对外沟通先请示。' },
  { value: 'normal', label: '普通', description: '主动处理相关风险与未闭环事项；明确无风险的沟通可按审批策略直接执行。' },
  { value: 'active', label: '活跃', description: '扩大有依据的主动处理与准备范围；明确低风险的沟通可按审批策略直接执行。' },
]

function isOtherPrompt(item: TextFile): boolean {
  return item.kind !== 'initiative_level' && item.stage !== 'm3' && item.stage !== 'm5'
}

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
  heartbeat: '本轮时间与巡视上下文',
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
  readOnly?: boolean
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
  readOnly = false,
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
        readOnly={readOnly}
        onChange={(event) => onChange(event.target.value)}
        autoSize={{ minRows: 12, maxRows: 28 }}
        placeholder={placeholder}
        className="agent-markdown-editor"
      />
      {!readOnly && <Flex justify="flex-end" style={{ marginTop: 12 }}>
        <Button type="primary" onClick={onSave} loading={saving} disabled={!value.trim()}>
          保存修改
        </Button>
      </Flex>}
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
          <div><Text type="secondary">使用已保存的主动程度与该阶段配置；工作规则、审批策略按所属阶段展开。下列动态内容在真实运行时注入。</Text></div>
        </div>
        <Tag>{initiativeLevels.find((item) => item.value === preview.initiative_level)?.label ?? preview.initiative_level}档</Tag>
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
  const [stageSections, setStageSections] = useState<Record<RuleStage, StageSection>>({
    m3: 'prompt',
    m5: 'prompt',
  })

  const reloadPreviews = useCallback(async () => {
    setPreviews({})
    const [m3, m5, proactive] = await Promise.all([getAgentConfigPreview('m3'), getAgentConfigPreview('m5'), getAgentConfigPreview('proactive')])
    setPreviews({ m3, m5, proactive })
  }, [])

  const reload = useCallback(() => {
    setLoading(true)
    Promise.all([listTextFiles(), listWorkRules(), getAgentConfigPreview('m3'), getAgentConfigPreview('m5'), getAgentConfigPreview('proactive')])
      .then(([fileResult, ruleResult, m3, m5, proactive]) => {
        setTextFiles(fileResult.items)
        setWorkRules(ruleResult.items)
        setTextDrafts(Object.fromEntries(fileResult.items.map((item) => [item.key, item.content])))
        setRuleDrafts(Object.fromEntries(ruleResult.items.map((item) => [item.key, item.content])))
        setPreviews({ m3, m5, proactive })
        const others = fileResult.items.filter(isOtherPrompt)
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
  const otherFiles = useMemo(() => textFiles.filter(isOtherPrompt), [textFiles])
  const initiative = filesByKey[initiativeKey]

  const refreshSavedPreviews = async () => {
    try {
      await reloadPreviews()
    } catch (cause: unknown) {
      setError(`配置已保存，但生效预览加载失败：${errorText(cause)}`)
    }
  }

  const saveInitiative = async (level: InitiativeLevel) => {
    if (savingKey || level === initiative?.content) return
    setSavingKey(`text:${initiativeKey}`)
    setError(undefined)
    setNotice(undefined)
    try {
      const updated = await updateTextFile(initiativeKey, { content: level })
      setTextFiles((current) => current.map((item) => item.key === initiativeKey ? updated : item))
      setTextDrafts((current) => ({ ...current, [initiativeKey]: updated.content }))
      setNotice('主动程度已保存，后续运行会实时读取，无需重启。')
      await refreshSavedPreviews()
    } catch (cause: unknown) {
      setError(`主动程度未保存：${errorText(cause)}`)
    } finally {
      setSavingKey(undefined)
    }
  }

  const saveText = async (key: string) => {
    if (savingKey) return
    const content = textDrafts[key] ?? ''
    if (!content.trim()) {
      setError(`${filesByKey[key]?.name ?? key}不能为空`)
      return
    }
    setSavingKey(`text:${key}`)
    setError(undefined)
    setNotice(undefined)
    try {
      const updated = await updateTextFile(key, { content })
      setTextFiles((current) => current.map((item) => item.key === key ? updated : item))
      setTextDrafts((current) => ({ ...current, [key]: updated.content }))
      setNotice(`${updated.name}已保存，后续运行会实时读取`)
      await refreshSavedPreviews()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingKey(undefined)
    }
  }

  const saveRule = async (key: WorkRule['key']) => {
    if (savingKey) return
    setSavingKey(`rule:${key}`)
    setError(undefined)
    setNotice(undefined)
    try {
      const updated = await updateWorkRule(key, { content: ruleDrafts[key] ?? '' })
      setWorkRules((current) => current.map((item) => item.key === key ? updated : item))
      setRuleDrafts((current) => ({ ...current, [key]: updated.content }))
      setNotice(`${updated.name}工作规则已保存，后续运行会实时读取`)
      await refreshSavedPreviews()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSavingKey(undefined)
    }
  }

  const stagePanel = (stage: RuleStage) => {
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
            description={`在这里编辑${stageName}的三档行为及共同工作规则；只注入本阶段。生效预览使用已保存的内容。`}
            path={stageRule.path}
            value={ruleDrafts[stageRuleKey] ?? ''}
            saving={savingKey === `rule:${stageRuleKey}`}
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
            : '模板必须各保留一个 {{WORK_RULES}} 和 {{APPROVAL_POLICY}}；初次执行、等待恢复和人工回答恢复使用同一套组装逻辑。'}当前主动程度由运行时自动注入。`}
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
      <PageHeader title="工作设定" subtitle="配置任务执行、线索发现及其他 Agent" />
      {error && <Alert type="error" showIcon title="工作设定操作失败" description={error} closable onClose={() => setError(undefined)} />}
      {notice && <Alert type="success" showIcon title={notice} closable onClose={() => setNotice(undefined)} />}
      <Spin spinning={loading}>
        <Card className="agent-config-card agent-initiative-card" variant="borderless">
          <div className="agent-config-card-heading">
            <div>
              <Text strong>主动程度</Text>
              <Text type="secondary">控制主动处理范围与审批尺度。三档都必须交付处理结果：群消息回原会话或话题，真人单聊在共同助手群回复，并始终 CC 你。正在处理的工作从下一轮采用新设置。</Text>
            </div>
          </div>
          <Radio.Group
            aria-label="主动程度"
            value={initiative?.content}
            onChange={(event) => void saveInitiative(event.target.value as InitiativeLevel)}
            disabled={loading || !!savingKey || !initiative}
            optionType="button"
            buttonStyle="solid"
            options={initiativeLevels.map(({ value, label }) => ({ value, label }))}
          />
          <div className="agent-initiative-description">
            <Text type="secondary">{initiativeLevels.find((item) => item.value === initiative?.content)?.description ?? '主动程度未加载'}</Text>
            {savingKey === `text:${initiativeKey}` && <Spin size="small" />}
          </div>
          <Text type="secondary">线索发现和任务执行的档位规则在各自「工作规则」中编辑；主动巡视在「其他 Agent」中编辑和预览。</Text>
        </Card>
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
                        <div className="agent-stage-content">
                          <MarkdownEditor
                            title={item.name}
                            description={item.stage === 'cc' ? '当前仅展示安装模板。飞书前台提示词在安装或绑定时写入 CC 配置，暂不支持后台编辑。' : item.description}
                            path={item.path}
                            value={textDrafts[item.key] ?? ''}
                            saving={savingKey === `text:${item.key}`}
                            readOnly={item.stage === 'cc'}
                            onChange={(value) => setTextDrafts((current) => ({ ...current, [item.key]: value }))}
                            onSave={() => saveText(item.key)}
                          />
                          {item.stage === 'proactive' && <EffectivePreview preview={previews.proactive} />}
                        </div>
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
