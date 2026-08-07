import { useCallback, useEffect, useState } from 'react'
import type { ReactNode } from 'react'
import {
  Alert,
  Button,
  Col,
  Collapse,
  Flex,
  Form,
  Input,
  InputNumber,
  Row,
  Select,
  Space,
  Spin,
  Switch,
  Tag,
  Tooltip,
  Typography,
} from 'antd'
import { QuestionCircleOutlined } from '@ant-design/icons'
import { getRuntimeSettings, updateRuntimeSettings } from './api'
import type { RuntimeSettings as RuntimeSettingsInput } from './types'

const { Text, Title } = Typography
type FieldName = keyof RuntimeSettingsInput

const fieldSpan = { xs: 24, sm: 12, lg: 8, xxl: 6 }
const cliOptions = [
  { value: 'codex', label: 'Codex CLI' },
  { value: 'traex', label: 'TraeX CLI' },
]
const reasoningOptions = ['minimal', 'low', 'medium', 'high', 'xhigh'].map((value) => ({ value, label: value }))
const sandboxOptions = [
  { value: 'read-only', label: '只读' },
  { value: 'workspace-write', label: '仅工作区可写' },
  { value: 'danger-full-access', label: '整机可读写' },
]

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function FieldLabel({ label, help }: { label: string; help?: string }) {
  if (!help) return label
  return (
    <Space size={4}>
      <span>{label}</span>
      <Tooltip title={help} placement="top">
        <QuestionCircleOutlined className="runtime-settings-help" />
      </Tooltip>
    </Space>
  )
}

function SettingCol({ children }: { children: ReactNode }) {
  return <Col {...fieldSpan}>{children}</Col>
}

function TextField({
  name,
  label,
  placeholder,
  help,
}: {
  name: FieldName
  label: string
  placeholder?: string
  help?: string
}) {
  return (
    <SettingCol>
      <Form.Item name={name} label={<FieldLabel label={label} help={help} />} rules={[{ required: true, whitespace: true }]}>
        <Input placeholder={placeholder} />
      </Form.Item>
    </SettingCol>
  )
}

function NumberField({
  name,
  label,
  min,
  max,
  step = 1,
  precision,
  help,
}: {
  name: FieldName
  label: string
  min: number
  max: number
  step?: number
  precision?: number
  help?: string
}) {
  return (
    <SettingCol>
      <Form.Item name={name} label={<FieldLabel label={label} help={help} />} rules={[{ required: true }]}>
        <InputNumber min={min} max={max} step={step} precision={precision} style={{ width: '100%' }} />
      </Form.Item>
    </SettingCol>
  )
}

function SelectField({
  name,
  label,
  options,
  help,
}: {
  name: FieldName
  label: string
  options: { value: string; label: string }[]
  help?: string
}) {
  return (
    <SettingCol>
      <Form.Item name={name} label={<FieldLabel label={label} help={help} />} rules={[{ required: true }]}>
        <Select options={options} />
      </Form.Item>
    </SettingCol>
  )
}

function SwitchField({
  name,
  label,
  extra,
  help,
}: {
  name: FieldName
  label: string
  extra?: string
  help?: string
}) {
  return (
    <SettingCol>
      <Form.Item name={name} label={<FieldLabel label={label} help={help} />} valuePropName="checked" extra={extra}>
        <Switch />
      </Form.Item>
    </SettingCol>
  )
}

function Section({
  title,
  description,
  children,
}: {
  title: string
  description?: string
  children: ReactNode
}) {
  return (
    <section className="runtime-settings-section">
      <div className="runtime-settings-section-heading">
        <Text strong>{title}</Text>
        {description && <Text type="secondary">{description}</Text>}
      </div>
      <Row gutter={[12, 0]}>{children}</Row>
    </section>
  )
}

function PanelLabel({ title, description }: { title: string; description: string }) {
  return (
    <div className="runtime-settings-panel-label">
      <span>{title}</span>
      <Text type="secondary">{description}</Text>
    </div>
  )
}

function RuntimeStep({
  stage,
  title,
  enabled,
  primary,
  secondary,
}: {
  stage: string
  title: string
  enabled: boolean
  primary: string
  secondary: string
}) {
  return (
    <div className={`runtime-step ${enabled ? '' : 'is-disabled'}`}>
      <Flex justify="space-between" align="center" gap={6}>
        <Text className="runtime-step-stage">{stage}</Text>
        <Tag variant="filled" color={enabled ? 'success' : 'default'}>{enabled ? '启用' : '停用'}</Tag>
      </Flex>
      <Text strong className="runtime-step-title">{title}</Text>
      <Text className="runtime-step-primary">{primary}</Text>
      <Text type="secondary" className="runtime-step-secondary">{secondary}</Text>
    </div>
  )
}

export default function RuntimeSettings() {
  const [form] = Form.useForm<RuntimeSettingsInput>()
  const [loading, setLoading] = useState(true)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [success, setSuccess] = useState<string>()
  const [dirty, setDirty] = useState(false)
  const [restartRequired, setRestartRequired] = useState(false)
  const [overridePath, setOverridePath] = useState('')
  const [liveSettings, setLiveSettings] = useState<RuntimeSettingsInput>()
  const executeAutoEnabled = Form.useWatch('execute_auto_enabled', form)

  const reload = useCallback(() => {
    const controller = new AbortController()
    setLoading(true)
    getRuntimeSettings(controller.signal)
      .then((view) => {
        form.setFieldsValue(view.settings)
        setLiveSettings(view.settings)
        setRestartRequired(view.restart_required)
        setOverridePath(view.override_path)
        setError(undefined)
        setSuccess(undefined)
        setDirty(false)
      })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [form])

  useEffect(reload, [reload])

  const save = async () => {
    await form.validateFields()
    // Collapse lazily registers panel fields, while this API replaces the
    // complete settings document. Include values loaded into the form store
    // even when their panel has never been expanded.
    const values = form.getFieldsValue(true) as RuntimeSettingsInput
    setSaving(true)
    try {
      const view = await updateRuntimeSettings(values)
      form.setFieldsValue(view.settings)
      setLiveSettings(view.settings)
      setRestartRequired(view.restart_required)
      setOverridePath(view.override_path)
      setError(undefined)
      setSuccess('运行配置已保存')
      setDirty(false)
    } catch (cause: unknown) {
      setSuccess(undefined)
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  if (loading || !liveSettings) {
    return <div style={{ padding: 32, textAlign: 'center' }}><Spin /></div>
  }

  const m3Runtime = liveSettings.extract_engine === 'model_api'
    ? `Model API · ${liveSettings.model_api_model}`
    : `${liveSettings.analysis_cli} · ${liveSettings.analysis_model}`

  const panels = [
    {
      key: 'common',
      label: <PanelLabel title="常用设置" description="总开关、CLI 和模型归属" />,
      children: (
        <>
          <Section title="阶段开关" description="控制后台自动运行；保存后需重启主服务。">
            <SwitchField name="extract_enabled" label="M3 自动提取" help="从新消息中识别行动线索并生成 Todo。" />
            <SwitchField name="execute_auto_enabled" label="M5 自动执行" help="自动固化 extracted Todo 并执行 Task；关闭后仍可手动执行 Task。" />
            <SwitchField name="proactive_enabled" label="主动巡视" help="启动两分钟后先巡视一次，之后按周期整理世界模型并发现可做之事。" />
            <SwitchField name="chat_enabled" label="右侧对话" help="启用页面右侧的 Jarvis 对话入口。" />
          </Section>
          <Section title="M3 Agent" description="M3 选择 Agent CLI 时使用这组 CLI、模型和超时。">
            <SelectField name="analysis_cli" label="M3 CLI" options={cliOptions} help="M3 提取启动的命令行执行器。" />
            <TextField name="analysis_model" label="M3 模型" help="传给 M3 Agent CLI 的模型名。" />
            <NumberField name="analysis_timeout_seconds" label="单次提取超时（秒）" min={30} max={3600} step={30} help="M3 每次 Agent 调用的最长运行时间。" />
          </Section>
          <Section title="Model API" description="当前用于 Todo 相似去重；M3 切到 Model API 后也用于线索提取。">
            <TextField name="model_api_model" label="去重 / 备用提取模型" help="当前使用火山 Ark 模型，不会替代 M5 执行或对话模型。" />
            <NumberField name="model_api_timeout_seconds" label="API 请求超时（秒）" min={10} max={600} step={10} help="Ark Model API 和向量 API 的 HTTP 请求超时。" />
          </Section>
          <Section title="M5 执行器" description="M5 使用独立 CLI；右侧对话复用该 CLI，但可另选模型。">
            <SelectField name="execute_cli" label="执行 CLI" options={cliOptions} help="M5 执行任务及右侧对话使用的命令行执行器。" />
            <TextField name="execute_model" label="M5 执行模型" />
            <SelectField name="execute_reasoning_effort" label="M5 推理档位" options={reasoningOptions} />
          </Section>
          <Section title="主动巡视 Agent" description="低成本 Agent 可更新内部世界模型；任何外部动作只能创建 Task 交给强 M5。">
            <SelectField name="proactive_cli" label="巡视 CLI" options={cliOptions} />
            <TextField name="proactive_model" label="巡视模型" />
            <SelectField name="proactive_reasoning_effort" label="推理档位" options={reasoningOptions} />
            <SelectField name="proactive_sandbox" label="文件权限" options={sandboxOptions} help="权限用于调查；对外写入仍受主动巡视提示词边界约束。" />
            <TextField name="proactive_schedule" label="巡视周期" placeholder="@every 1h" />
            <NumberField name="proactive_startup_delay_seconds" label="启动后首次运行（秒）" min={1} max={3600} />
            <NumberField name="proactive_timeout_seconds" label="单轮超时（秒）" min={30} max={3600} step={30} />
          </Section>
        </>
      ),
    },
    {
      key: 'extract',
      label: <PanelLabel title="M3 · 线索提取" description="消息 → Todo；引擎、上下文、去重和工具" />,
      children: (
        <>
          <Section title="运行方式" description="选择提取引擎，并控制 Agent 的权限和补偿扫描。">
            <SelectField
              name="extract_engine"
              label="提取引擎"
              options={[
                { value: 'codex', label: 'Agent CLI（推荐）' },
                { value: 'model_api', label: 'Model API 工具循环' },
              ]}
              help="Agent CLI 可主动调用 lark-cli、git 等工具；Model API 只运行内置工具循环。"
            />
            <SelectField name="extract_reasoning_effort" label="推理档位" options={reasoningOptions} help="仅在提取引擎为 Agent CLI 时生效。" />
            <SelectField name="extract_sandbox" label="文件权限" options={sandboxOptions} help="仅限制 M3 Agent CLI 子进程。" />
            <SwitchField name="extract_network_enabled" label="允许联网" help="允许 M3 Agent 查询飞书、代码平台和外部资料。" />
            <TextField name="extract_schedule" label="补偿扫描周期" placeholder="@every 10m" help="事件触发遗漏时，按此周期扫描待提取消息。" />
            <NumberField name="extract_concurrency" label="并发会话数" min={1} max={16} help="不同单聊或群聊可并行；同一个 chat_id 始终串行。" />
            <NumberField name="extract_batch_messages" label="每批消息上限" min={1} max={5000} />
          </Section>
          <Section title="输入上下文" description="决定每次提取能看到多少近期消息、开放 Todo、今天的事实明细、关键人事实和最近有进展的任务。">
            <NumberField name="extract_context_messages" label="每个会话前文条数" min={0} max={500} />
            <NumberField name="extract_context_window_minutes" label="前文时间窗（分钟）" min={1} max={10080} />
            <NumberField name="extract_open_todo_limit" label="开放 Todo 上限" min={1} max={1000} help="随 Prompt 提供的未关闭 Todo 数量，用于避免重复创建。" />
            <NumberField name="extract_fact_limit" label="每主体今天事实上限" min={1} max={100} help="每个主体（群/项目/人）今天注入的明细事实条数；前一天另加一条日压缩摘要。" />
            <NumberField name="extract_key_person_limit" label="关键人事实人数上限" min={1} max={50} help="交办人、leader 与本轮发言者取并集后，最多取多少人注入人物事实。" />
            <NumberField name="extract_recent_task_limit" label="最近有进展任务上限" min={1} max={100} help="注入近期有进展的任务摘要条数。" />
            <NumberField name="extract_max_prompt_chars" label="Prompt 字符上限" min={1000} max={1000000} step={1000} />
          </Section>
          <Section title="语义去重" description="先查相似 Todo；非精确命中时再由 Model API 判断是否同一行动。">
            <NumberField name="extract_semantic_threshold" label="相似度门槛" min={0.01} max={1} step={0.01} precision={2} help="越高越严格；达到门槛的相似 Todo 才进入模型裁决。" />
            <NumberField name="extract_semantic_neighbor_limit" label="候选 Todo 数" min={1} max={100} help="最多交给去重模型检查的相似 Todo 数量。" />
            <NumberField name="extract_evidence_retry_max" label="证据补抽次数" min={0} max={10} help="来源原文无法逐字匹配时，允许重新提取的次数。" />
          </Section>
          <Section title="Model API 工具循环" description="仅在提取引擎为 Model API 时控制循环；Agent CLI 不受这些参数限制。">
            <NumberField name="extract_tool_timeout_seconds" label="单个工具超时（秒）" min={1} max={600} />
            <NumberField name="extract_history_tool_limit" label="历史消息返回上限" min={1} max={1000} />
          </Section>
        </>
      ),
    },
    {
      key: 'execute',
      label: <PanelLabel title="M5 · 任务执行与对话" description="Task 执行、并发恢复和右侧对话" />,
      children: (
        <>
          <Section title="任务执行" description="CLI 和模型在“常用设置”中配置。">
            <TextField name="execute_schedule" label="补偿扫描周期" placeholder="@every 5m" />
            <NumberField name="execute_batch_limit" label="每批 Task 上限" min={1} max={100} />
            <NumberField name="execute_concurrency" label="最大并发任务" min={1} max={16} />
            <NumberField name="execute_timeout_seconds" label="单个任务超时（秒）" min={60} max={7200} step={60} />
            <SettingCol>
              <Form.Item
                name="execute_stale_minutes"
                label={<FieldLabel label="中断任务判定（分钟）" help="任务超过该时长仍处于 executing，重启恢复时将其标记为失败。" />}
                dependencies={['execute_timeout_seconds']}
                rules={[
                  { required: true },
                  ({ getFieldValue }) => ({
                    validator(_, value: number) {
                      const timeout = Number(getFieldValue('execute_timeout_seconds') || 0)
                      return value * 60 > timeout
                        ? Promise.resolve()
                        : Promise.reject(new Error('必须大于单个任务超时'))
                    },
                  }),
                ]}
              >
                <InputNumber min={2} max={240} step={5} style={{ width: '100%' }} />
              </Form.Item>
            </SettingCol>
          </Section>
          <Section title="右侧对话" description="复用 M5 的执行 CLI，但模型、权限和超时独立配置。">
            <TextField name="chat_model" label="对话模型" />
            <SelectField name="chat_reasoning_effort" label="推理档位" options={reasoningOptions} />
            <SelectField name="chat_sandbox" label="文件权限" options={sandboxOptions} />
            <NumberField name="chat_timeout_seconds" label="单轮超时（秒）" min={30} max={3600} step={30} />
          </Section>
        </>
      ),
    },
    {
      key: 'capture-facts',
      label: <PanelLabel title="采集与世界模型" description="飞书消息和持续世界建模 Agent" />,
      children: (
        <>
          <Section title="消息采集" description="发现可处理的会话，并增量扫描飞书消息。">
            <TextField name="capture_discover_schedule" label="发现会话周期" placeholder="@every 6h" />
            <TextField name="capture_scan_schedule" label="扫描消息周期" placeholder="@every 5m" />
            <NumberField name="capture_page_size" label="飞书单页消息数" min={1} max={50} />
            <NumberField name="capture_scan_workers" label="并发扫描会话数" min={1} max={32} />
            <NumberField name="capture_auto_related_p2p_top_n" label="自动关注私聊数" min={0} max={500} help="按近期活跃度自动纳入采集的私聊数量；0 表示关闭。" />
          </Section>
          <Section title="持续世界建模" description="在主流水线之外增量阅读消息、Todo 和 Task，由 Agent 自主维护人物、项目、群、资料、关系与历史事实；并按天压缩事实阅读层。">
            <SwitchField name="fact_engine_enabled" label="自动世界建模" />
            <TextField name="fact_engine_schedule" label="建模周期" placeholder="@every 15m" />
            <TextField name="fact_engine_rollup_schedule" label="日压缩周期" placeholder="0 2 * * *" help="每天把前一个自然日每个主体的明细事实压成一条摘要，供 M3 提示词使用。" />
            <TextField name="fact_engine_model" label="世界维护模型" />
            <TextField name="fact_engine_rollup_model" label="事实日压缩模型" />
            <NumberField name="fact_engine_timeout_seconds" label="单轮超时（秒）" min={1} max={3600} />
            <NumberField name="fact_engine_batch_limit" label="每来源候选行上限" min={1} max={5000} />
            <NumberField name="fact_engine_max_material_chars" label="单次材料字符上限" min={1} max={1000000} />
            <NumberField name="fact_engine_window_gap_minutes" label="新窗口间隔（分钟）" min={1} max={1440} help="同一会话相邻消息超过该间隔时拆成两个事实窗口。" />
            <NumberField name="fact_engine_window_max_messages" label="每个窗口消息上限" min={1} max={1000} />
          </Section>
        </>
      ),
    },
    {
      key: 'automation',
      label: <PanelLabel title="自动任务、日报与飞书调用" description="定时唤醒、日报生成和 lark-cli 限流" />,
      children: (
        <>
          <Section title="定时任务" description="到期后创建 Task；依赖 M5 自动执行。">
            <SettingCol>
              <Form.Item
                name="scheduled_task_enabled"
                label={<FieldLabel label="自动执行到期任务" help="关闭后仍保存定时任务，但不会自动提交给 M5。" />}
                valuePropName="checked"
                dependencies={['execute_auto_enabled']}
                rules={[
                  ({ getFieldValue }) => ({
                    validator(_, value: boolean) {
                      return !value || getFieldValue('execute_auto_enabled')
                        ? Promise.resolve()
                        : Promise.reject(new Error('请先启用 M5 自动执行'))
                    },
                  }),
                ]}
                extra={executeAutoEnabled ? undefined : '请先启用 M5 自动执行'}
              >
                <Switch />
              </Form.Item>
            </SettingCol>
            <TextField name="scheduled_task_schedule" label="到期扫描周期" placeholder="@every 1m" />
            <NumberField name="scheduled_task_batch_limit" label="每批任务上限" min={1} max={1000} />
          </Section>
          <Section title="每日进度" description="定时汇总个人和相关群的当日进展。">
            <SwitchField name="daily_digest_enabled" label="定时生成日报" extra="手动生成不受影响" />
            <TextField name="daily_digest_schedule" label="生成时间" placeholder="0 19 * * *" />
            <NumberField name="daily_digest_message_limit" label="每个群消息上限" min={1} max={5000} />
            <NumberField name="daily_digest_concurrency" label="并发总结群数" min={1} max={32} />
          </Section>
          <Section title="lark-cli 调用限制" description="所有后台模块共用，避免飞书 API 突发限流。">
            <NumberField name="lark_rate_limit" label="持续速率（次/秒）" min={0.1} max={100} step={0.5} precision={1} />
            <NumberField name="lark_burst" label="瞬时突发容量" min={1} max={1000} />
            <NumberField name="lark_concurrency" label="最大并发请求" min={1} max={32} />
            <NumberField name="lark_timeout_seconds" label="单次调用超时（秒）" min={1} max={600} />
          </Section>
        </>
      ),
    },
  ]

  return (
    <div className="runtime-settings">
      <Flex className="runtime-settings-header" justify="space-between" align="flex-start" gap={16}>
        <div>
          <Title level={4}>运行配置</Title>
          <Text type="secondary">按处理阶段配置 Agent、调度、权限和吞吐。保存只写入本机覆盖文件。</Text>
        </div>
        <Space size={8}>
          <Button onClick={reload} disabled={saving}>重读</Button>
          <Button type="primary" onClick={save} loading={saving} disabled={!dirty}>保存配置</Button>
        </Space>
      </Flex>

      {restartRequired && (
        <Alert
          type="warning"
          showIcon
          title="配置已保存，重启主服务后生效"
          description="运行 ./scripts/rebuild-server.sh 重新构建并重启。"
        />
      )}
      {success && <Alert type="success" showIcon title={success} closable onClose={() => setSuccess(undefined)} />}
      {error && <Alert type="error" showIcon title="保存失败" description={error} closable onClose={() => setError(undefined)} />}

      <div className="runtime-settings-flow">
        <RuntimeStep
          stage="M3"
          title="线索提取"
          enabled={liveSettings.extract_enabled}
          primary={m3Runtime}
          secondary={`${liveSettings.extract_schedule} · ${liveSettings.extract_concurrency} 并发 · 最多 ${liveSettings.extract_batch_messages} 条`}
        />
        <RuntimeStep
          stage="PULSE"
          title="主动巡视"
          enabled={liveSettings.proactive_enabled}
          primary={`${liveSettings.proactive_cli} · ${liveSettings.proactive_model}`}
          secondary={`${liveSettings.proactive_schedule} · 启动后 ${liveSettings.proactive_startup_delay_seconds}s`}
        />
        <RuntimeStep
          stage="M5"
          title="任务执行"
          enabled={liveSettings.execute_auto_enabled}
          primary={`${liveSettings.execute_cli} · ${liveSettings.execute_model}`}
          secondary={`${liveSettings.execute_concurrency} 并发 · ${liveSettings.execute_timeout_seconds}s 超时`}
        />
        <RuntimeStep
          stage="CHAT"
          title="右侧对话"
          enabled={liveSettings.chat_enabled}
          primary={`${liveSettings.execute_cli} · ${liveSettings.chat_model}`}
          secondary={`${liveSettings.chat_reasoning_effort} · ${liveSettings.chat_timeout_seconds}s 超时`}
        />
        <RuntimeStep
          stage="FACT"
          title="长期事实"
          enabled={liveSettings.fact_engine_enabled}
          primary={liveSettings.fact_engine_model}
          secondary={`${liveSettings.fact_engine_schedule} · 单次最多 ${liveSettings.fact_engine_max_material_chars} 字符`}
        />
      </div>

      <Flex className="runtime-settings-meta" justify="space-between" align="center" gap={12}>
        <Text type="secondary">常用项默认展开；阶段细节和低频参数按需调整。</Text>
        <Tooltip title={overridePath}><Text type="secondary">本机覆盖文件</Text></Tooltip>
      </Flex>

      <Form
        form={form}
        layout="vertical"
        requiredMark={false}
        size="small"
        onValuesChange={() => {
          setLiveSettings(form.getFieldsValue(true) as RuntimeSettingsInput)
          setDirty(true)
          setSuccess(undefined)
        }}
      >
        <Collapse size="small" defaultActiveKey={['common']} items={panels} />
      </Form>

      {dirty && (
        <div className="runtime-settings-actions" role="status" aria-live="polite">
          <Text strong>有未保存的运行配置</Text>
          <Space size={8}>
            <Button onClick={reload} disabled={saving}>放弃修改</Button>
            <Button type="primary" onClick={save} loading={saving}>保存配置</Button>
          </Space>
        </div>
      )}
    </div>
  )
}
