import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Collapse, Descriptions, Flex, Form, Input, Tag, Typography } from 'antd'
import { getProfile, resolvePerson, updateProfile } from '../api'
import FactTimeline from '../world/FactTimeline'
import SummaryPageEditor from '../world/SummaryPageEditor'
import type { ProfileInput, ProfileView, ResolveCandidate } from '../types'
import errorText from './errorText'

const { Text } = Typography

export default function ProfilePanel() {
  const [profile, setProfile] = useState<ProfileView | null>(null)
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [ok, setOk] = useState(false)
  const [form] = Form.useForm<ProfileInput>()

  // Leader binding reuses the person search so leader_open_id is a real open_id.
  const [leaderOpenID, setLeaderOpenID] = useState('')
  const [leaderName, setLeaderName] = useState('')
  const [leaderQuery, setLeaderQuery] = useState('')
  const [leaderSearching, setLeaderSearching] = useState(false)
  const [leaderCandidates, setLeaderCandidates] = useState<ResolveCandidate[] | null>(null)

  const reload = useCallback(() => {
    setLoading(true)
    getProfile()
      .then((result) => {
        setProfile(result)
        setLeaderOpenID(result.leader_open_id || '')
        setLeaderName(result.leader_name || '')
        form.setFieldsValue({
          name: result.name, department: result.department, title: result.title,
        })
        setError(undefined)
      })
      .catch((cause: unknown) => setError(errorText(cause)))
      .finally(() => setLoading(false))
  }, [form])
  useEffect(reload, [reload])

  const runLeaderSearch = async () => {
    if (!leaderQuery.trim()) return
    setLeaderSearching(true)
    try {
      const result = await resolvePerson(leaderQuery.trim())
      setLeaderCandidates(result.candidates)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setLeaderSearching(false)
    }
  }
  const pickLeader = (candidate: ResolveCandidate) => {
    setLeaderOpenID(candidate.open_id)
    setLeaderName(candidate.name)
    setLeaderCandidates(null)
    setLeaderQuery('')
  }
  const clearLeader = () => { setLeaderOpenID(''); setLeaderName('') }

  const submit = async () => {
    const values = await form.validateFields()
    setSaving(true)
    setOk(false)
    try {
      const saved = await updateProfile({
        ...values,
        leader_open_id: leaderOpenID || null,
        leader_name: leaderName || null,
      })
      setProfile(saved)
      setOk(true)
      setError(undefined)
    } catch (cause: unknown) {
      setError(errorText(cause))
    } finally {
      setSaving(false)
    }
  }

  return <>
    {error && <Alert type="error" showIcon title="保存失败" description={error} closable onClose={() => setError(undefined)} style={{ marginBottom: 12 }} />}
    {ok && <Alert type="success" showIcon title="已保存，抽取时会把「我的背景」喂给模型" closable onClose={() => setOk(false)} style={{ marginBottom: 12 }} />}
    {profile && !profile.saved && <Alert type="info" showIcon title="首次填写：Principal（我）背景尚未设置，完善后可显著提升 leader 软措辞交办的识别" style={{ marginBottom: 12 }} />}
    <Card variant="borderless" loading={loading} className="memory-profile-card">
      <Form form={form} layout="vertical">
        <Form.Item name="name" label="姓名（当前用户是谁）" rules={[{ required: true, message: '请填写姓名' }]}>
          <Input placeholder="如：负责人姓名" />
        </Form.Item>
        <Flex gap={12}>
          <Form.Item name="department" label="部门" style={{ flex: 1 }}><Input placeholder="选填" /></Form.Item>
          <Form.Item name="title" label="职位" style={{ flex: 1 }}><Input placeholder="选填" /></Form.Item>
        </Flex>
        <Form.Item label="直属 leader" tooltip="显式告诉模型「我的 leader 是谁」，对识别 leader 软措辞交办最关键">
          {leaderOpenID ? (
            <Flex gap={8} align="center">
              <Tag color="gold">{leaderName || leaderOpenID}</Tag>
              <Button size="small" onClick={clearLeader}>清除</Button>
            </Flex>
          ) : (
            <Flex vertical gap={8}>
              <Flex gap={8}>
                <Input.Search
                  placeholder="搜索姓名 / 邮箱绑定 leader" value={leaderQuery}
                  onChange={(e) => setLeaderQuery(e.target.value)} onSearch={runLeaderSearch}
                  loading={leaderSearching} enterButton="搜索" style={{ maxWidth: 360 }}
                />
              </Flex>
              {leaderCandidates && (
                <Card size="small" variant="outlined">
                  {leaderCandidates.length === 0 ? <Text type="secondary">无匹配</Text> : leaderCandidates.map((c) => (
                    <Flex key={c.open_id} justify="space-between" align="center" style={{ padding: '4px 0' }}>
                      <Text>{c.name} <Text type="secondary" style={{ fontSize: 12 }}>{c.department}</Text></Text>
                      <Button size="small" type="link" onClick={() => pickLeader(c)}>选择</Button>
                    </Flex>
                  ))}
                </Card>
              )}
            </Flex>
          )}
        </Form.Item>
        <Collapse
          ghost
          className="memory-advanced"
          items={[{
            key: 'identity',
            label: '高级信息',
            children: (
              <Descriptions size="small" column={1}>
                <Descriptions.Item label="我的飞书用户标识"><Text copyable>{profile?.open_id || '—'}</Text></Descriptions.Item>
                <Descriptions.Item label="直属 leader 用户标识"><Text copyable>{leaderOpenID || '—'}</Text></Descriptions.Item>
              </Descriptions>
            ),
          }]}
        />
        <Button type="primary" loading={saving} onClick={submit}>保存</Button>
      </Form>
    </Card>
    {profile?.saved && profile.id > 0 && (
      <SummaryPageEditor type="principal" id={profile.id} />
    )}
    {profile?.saved && profile.id > 0 && (
      <FactTimeline subject={{ type: 'principal', id: profile.id }} title="我的事实" />
    )}
  </>
}
