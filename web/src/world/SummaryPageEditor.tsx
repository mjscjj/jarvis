import { useCallback, useEffect, useState } from 'react'
import { Alert, Button, Card, Flex, Input, Spin, Tag, Typography } from 'antd'
import { getPage, isPageConflictError, updatePage } from '../api'
import type { PageLink, PageType, PageView } from '../types'
import { countChars, summaryMeterTone } from './summary'

const { Text } = Typography

function errorText(cause: unknown): string {
  return cause instanceof Error ? cause.message : String(cause)
}

function linkLabel(link: PageLink): string {
  return link.name ? `${link.name} (${link.type}:${link.id})` : `${link.type}:${link.id}`
}

export default function SummaryPageEditor({ type, id }: { type: PageType; id: number }) {
  const [page, setPage] = useState<PageView>()
  const [draft, setDraft] = useState('')
  const [loading, setLoading] = useState(false)
  const [saving, setSaving] = useState(false)
  const [error, setError] = useState<string>()
  const [conflict, setConflict] = useState<PageView>()
  const [saved, setSaved] = useState(false)

  const load = useCallback(() => {
    const controller = new AbortController()
    setLoading(true)
    setError(undefined)
    setConflict(undefined)
    getPage(type, id, controller.signal)
      .then((result) => {
        setPage(result)
        setDraft(result.summary ?? '')
      })
      .catch((cause: unknown) => {
        if (!(cause instanceof DOMException && cause.name === 'AbortError')) setError(errorText(cause))
      })
      .finally(() => {
        if (!controller.signal.aborted) setLoading(false)
      })
    return () => controller.abort()
  }, [type, id])

  useEffect(() => load(), [load])

  const applyCurrent = (current: PageView) => {
    setPage(current)
    setDraft(current.summary ?? '')
    setConflict(undefined)
  }

  const save = async () => {
    if (!page) return
    setSaving(true)
    setSaved(false)
    setError(undefined)
    try {
      const result = await updatePage(type, id, {
        content: draft,
        if_unchanged_since: page.updated_at,
      })
      setPage(result)
      setDraft(result.summary ?? '')
      setConflict(undefined)
      setSaved(true)
    } catch (cause: unknown) {
      if (isPageConflictError(cause)) {
        setConflict(cause.current)
      } else {
        setError(errorText(cause))
      }
    } finally {
      setSaving(false)
    }
  }

  if (loading && !page) {
    return <Card className="summary-page-editor" variant="borderless"><div className="summary-page-loading"><Spin /></div></Card>
  }

  const maxChars = page?.max_chars ?? 0
  const charCount = countChars(draft)
  const tone = summaryMeterTone(charCount, maxChars)
  const overLimit = maxChars > 0 && charCount > maxChars

  return (
    <Card className="summary-page-editor" variant="borderless">
      <Flex justify="space-between" align="flex-start" gap={12} wrap className="summary-page-heading">
        <div>
          <Text strong>长期事实</Text>
          <div>
            <Text type="secondary">第一行用一句话说明这是什么；后面写当前状态。细节留在事实流。</Text>
          </div>
        </div>
        {page && (
          <Text
            className={`summary-page-meter summary-page-meter-${tone}`}
            type={tone === 'ok' ? 'secondary' : undefined}
          >
            {charCount} / {maxChars}
          </Text>
        )}
      </Flex>
      {error && <Alert type="error" showIcon title="长期事实保存失败" description={error} closable onClose={() => setError(undefined)} />}
      {saved && <Alert type="success" showIcon title="长期事实已保存" closable onClose={() => setSaved(false)} />}
      {conflict && (
        <Alert
          type="warning"
          showIcon
          title="内容已被其他人更新"
          description={
            <div className="summary-page-conflict">
              <Text>当前版本如下。编辑器里的草稿没有被覆盖。</Text>
              <Input.TextArea value={conflict.summary ?? ''} readOnly autoSize={{ minRows: 4, maxRows: 12 }} className="summary-page-conflict-body" />
              <Flex gap={8}>
                <Button size="small" onClick={() => applyCurrent(conflict)}>改用当前版本</Button>
                <Button size="small" onClick={() => { setPage(conflict); setConflict(undefined) }}>保留草稿并继续</Button>
              </Flex>
            </div>
          }
        />
      )}
      <Input.TextArea
        value={draft}
        onChange={(event) => { setDraft(event.target.value); setSaved(false) }}
        autoSize={{ minRows: 10, maxRows: 28 }}
        placeholder={'第一行：这是什么\n后面：当前状态、结论，以及对其它实体或事实的引用'}
        className="summary-page-textarea"
        disabled={!page}
      />
      {overLimit && (
        <Text type="danger" className="summary-page-over-limit">
          已超过上限。请先压缩后再保存：合并旧明细为一句结论、把某一节改成对事实的引用，或删除已不重要的内容。
        </Text>
      )}
      {page && (
        <Flex gap={8} wrap className="summary-page-meta">
          <Text type="secondary">事实 {page.fact_count} 条</Text>
          {page.outgoing.length > 0 && (
            <Flex gap={4} wrap align="center">
              <Text type="secondary">出链</Text>
              {page.outgoing.map((link) => <Tag key={`out-${link.type}-${link.id}`}>{linkLabel(link)}</Tag>)}
            </Flex>
          )}
          {page.backlinks.length > 0 && (
            <Flex gap={4} wrap align="center">
              <Text type="secondary">反链</Text>
              {page.backlinks.map((link) => <Tag key={`back-${link.type}-${link.id}`}>{linkLabel(link)}</Tag>)}
            </Flex>
          )}
        </Flex>
      )}
      <Flex justify="flex-end" gap={8}>
        <Button onClick={() => load()} disabled={saving || loading}>重新加载</Button>
        <Button type="primary" onClick={save} loading={saving} disabled={!page}>保存长期事实</Button>
      </Flex>
    </Card>
  )
}
