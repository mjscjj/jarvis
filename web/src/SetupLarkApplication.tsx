import { useState } from 'react'
import { Alert, Button, Input } from 'antd'
import { getSetupPermissionConfig } from './api'
import type { SetupStatus } from './types'

export function larkApplicationURL(appId: string) {
  return `https://open.feishu.cn/app/${encodeURIComponent(appId)}`
}

// Keep application configuration separate from personal OAuth and secret repair.
export function SetupLarkApplication({ lark, disabled }: { lark: SetupStatus['lark']; disabled: boolean }) {
  const [permissionConfig, setPermissionConfig] = useState('')
  const [loading, setLoading] = useState(false)
  const [error, setError] = useState('')
  const loadPermissions = async () => {
    setLoading(true); setError('')
    try { setPermissionConfig(JSON.stringify(await getSetupPermissionConfig(), null, 2)) }
    catch (cause) { setError(cause instanceof Error ? cause.message : String(cause)) }
    finally { setLoading(false) }
  }
  return <section className="setup-application" aria-label="飞书应用配置">
    <p>在上方应用的管理页面一次补齐以下配置，完成后点击“重新检查”。</p>
    <ol>
      <li>启用机器人。在“权限管理”中选择“批量导入/导出权限”，导入下方清单并申请开通。清单包含消息、文档、日历、会议、妙记和待办所需权限。</li>
      <li>在“事件与回调”中配置长连接接收，订阅消息事件 <code>im.message.receive_v1</code> 和卡片回调 <code>card.action.trigger</code>。</li>
      <li>创建并发布应用版本，按企业要求完成审批。个人扫码授权不能替代这些应用配置。</li>
    </ol>
    <Button disabled={disabled} loading={loading} onClick={() => void loadPermissions()}>查看完整权限配置</Button>
    {error && <Alert type="error" showIcon message="读取权限配置失败" description={error} />}
    {permissionConfig && <>
      <p>复制全部内容，粘贴到当前应用的权限导入框：</p>
      <Input.TextArea aria-label="完整权限配置" value={permissionConfig} readOnly autoSize={{ minRows: 5, maxRows: 10 }} onFocus={event => event.target.select()} />
    </>}
    {lark.application_checks.map(check => <div key={check.event} className="setup-application-check">
      <span>{check.ready ? '已就绪' : '未就绪'}：{check.event}</span>
      {check.error && <details><summary>查看具体检查结果</summary><pre>{check.error}</pre></details>}
    </div>)}
  </section>
}
