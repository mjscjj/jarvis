import { useState } from 'react'
import { retryCommentNotifications } from '../api'
import type { CommentDelivery, PageComment } from '../types'

export function CommentDeliveryStatus({ comment }: { comment: PageComment }) {
 const [updated, setUpdated] = useState<CommentDelivery[] | null>(null)
 const [busy, setBusy] = useState(false)
 const [error, setError] = useState('')
 const items = updated ?? comment.notifications ?? []
 if (!items.length) return null
 const retry = async (email: string, resendUnknown = false) => {
	if (resendUnknown && !window.confirm('原消息可能已经送达，重新提醒可能产生重复消息。确认重新发送吗？')) return
  setBusy(true); setError('')
  try { setUpdated(await retryCommentNotifications(comment.id, email, resendUnknown)) }
  catch (cause) { setError(cause instanceof Error ? cause.message : '提醒重试失败') }
  finally { setBusy(false) }
 }
 return <div className="mt-1 text-[10px] text-slate-500">
  {items.map((item) => <div key={item.email}>
   {item.name}{item.reason === 'mention_and_owner' ? '（负责人 + @）' : item.reason === 'owner' ? '（负责人）' : item.reason === 'mention' ? '（@）' : ''}：{item.status === 'delivered' ? '已提醒' : item.status === 'pending' ? '等待提醒' : item.status === 'sending' ? '正在提醒' : item.status === 'unknown' ? '发送结果待核验' : '提醒未完成'}
   {item.error && <span className="ml-1 text-amber-700">{item.error}</span>}
   {item.status === 'failed' && <button type="button" disabled={busy} onClick={() => void retry(item.email)} className="ml-2 text-indigo-600 disabled:opacity-50">重试提醒</button>}
   {item.status === 'unknown' && item.message_id && <button type="button" disabled={busy} onClick={() => void retry(item.email)} className="ml-2 text-indigo-600">核验结果</button>}
	 {item.status === 'unknown' && !item.message_id && <button type="button" disabled={busy} onClick={() => void retry(item.email, true)} className="ml-2 text-indigo-600">重新提醒</button>}
  </div>)}
  {error && <div className="text-amber-700">{error}</div>}
 </div>
}
