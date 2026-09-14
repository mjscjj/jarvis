import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Flex, Form, Input, Modal, Select, Switch, Table, Tag, Typography } from 'antd'
import type { TableColumnsType } from 'antd'
import { getSkillContent, listSkills, scanSkills, updateSkill } from '../api'
import type { AgentSkill, AgentSkillInput, SkillStage, WorkRuleStage } from '../types'
import errorText from './errorText'

const { Text } = Typography

const workRuleStageLabels: Record<WorkRuleStage, string> = {
  extract: 'M3 抽取 Todo', execute: 'M5 执行',
}

export default function SkillsPanel() {
  const [items, setItems] = useState<AgentSkill[]>([])
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState<string>()
  const [editing, setEditing] = useState<AgentSkill | null>(null)
  const [content, setContent] = useState<{ name: string; path: string; text: string } | null>(null)
  const [submitting, setSubmitting] = useState(false)
  const [form] = Form.useForm<AgentSkillInput>()

  const reload = useCallback(() => {
    setLoading(true)
    listSkills()
      .then((result) => { setItems(result.items); setError(undefined) })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [])
  useEffect(reload, [reload])

  const sync = async () => {
    setLoading(true)
    try {
      const result = await scanSkills()
      setItems(result.items)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setLoading(false)
    }
  }
  const openEdit = (item: AgentSkill) => {
    setEditing(item)
    form.setFieldsValue({ stages: item.stages, is_enabled: item.is_enabled })
  }
  const submit = async () => {
    if (!editing) return
    const values = await form.validateFields()
    setSubmitting(true)
    try {
      await updateSkill(editing.name, values)
      setEditing(null)
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSubmitting(false)
    }
  }
  const toggle = async (item: AgentSkill, checked: boolean) => {
    try {
      await updateSkill(item.name, { stages: item.stages, is_enabled: checked })
      reload()
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }
  const showContent = async (item: AgentSkill) => {
    try {
      const result = await getSkillContent(item.name)
      setContent({ name: result.name, path: result.path, text: result.content })
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    }
  }

  const columns: TableColumnsType<AgentSkill> = [
    { title: 'Skill', dataIndex: 'name', width: 220, render: (name: string) => <Text strong>{name}</Text> },
    { title: '说明', dataIndex: 'description', ellipsis: true },
    {
      title: '目录展示阶段', width: 270,
      render: (_, item) => <Flex gap={4} wrap>{item.stages.map((stage) => <Tag key={stage}>{stage === 'proactive' ? '主动巡视' : workRuleStageLabels[stage]}</Tag>)}</Flex>,
    },
    {
      title: '加入目录', dataIndex: 'is_enabled', width: 100, align: 'center',
      render: (enabled: boolean, item) => <Switch size="small" checked={enabled} onChange={(checked) => toggle(item, checked)} />,
    },
    {
      title: '操作', width: 150, render: (_, item) => (
        <Flex gap={8}>
          <Button size="small" onClick={() => openEdit(item)}>范围</Button>
          <Button size="small" onClick={() => showContent(item)}>查看</Button>
        </Flex>
      ),
    },
  ]

  return <>
    <Flex justify="space-between" align="center" className="section-heading">
      <Text type="secondary">共 {items.length} 个 Skill，正文来自 .agents/skills；开关只控制自动加入 Agent 目录，关闭后仍可按名称读取，插件停用时不可用。</Text>
      <Flex gap={8}><Button onClick={reload} loading={loading}>刷新</Button><Button type="primary" onClick={sync} loading={loading}>扫描目录</Button></Flex>
    </Flex>
    {error && <Alert type="error" showIcon title="Skills 操作失败" description={error} closable onClose={() => setError(undefined)} />}
    <Card className="table-card" variant="borderless"><Table<AgentSkill> rowKey="name" columns={columns} dataSource={items} loading={loading} pagination={false} /></Card>
    <Modal title={`设置 Skill 目录展示 · ${editing?.name || ''}`} open={Boolean(editing)} confirmLoading={submitting} onOk={submit} onCancel={() => setEditing(null)} okText="保存" destroyOnHidden>
      <Form form={form} layout="vertical">
        <Form.Item name="stages" label="目录展示阶段" rules={[{ required: true, type: 'array', min: 1, message: '至少选择一个阶段' }]}>
          <Select mode="multiple" options={Object.entries({ ...workRuleStageLabels, proactive: '主动巡视' }).map(([value, label]) => ({ value: value as SkillStage, label }))} />
        </Form.Item>
        <Form.Item name="is_enabled" label="自动加入 Agent 目录" valuePropName="checked"><Switch /></Form.Item>
      </Form>
    </Modal>
    <Modal title={`Skill · ${content?.name || ''}`} open={Boolean(content)} onCancel={() => setContent(null)} footer={null} width={760}>
      <div style={{ marginBottom: 8 }}><Text code>{content?.path}</Text></div>
      <Input.TextArea value={content?.text} readOnly autoSize={{ minRows: 12, maxRows: 24 }} style={{ fontFamily: 'monospace' }} />
    </Modal>
  </>
}
