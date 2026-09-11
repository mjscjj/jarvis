import { Alert, Button, Descriptions, Flex, Typography } from 'antd'
import dayjs from 'dayjs'

import { useAppUpdate } from './AppUpdate'
import { updateStatusLine } from './updateStatus'

const { Text, Title } = Typography

export default function AboutSettings() {
  const { supported, currentVersion, status, update, error, checkedAt, check, openPrompt } = useAppUpdate()

  if (!supported) {
    return (
      <Alert
        type="info"
        showIcon
        title="当前通过浏览器访问本机服务"
        description="版本号与安装包升级由桌面应用管理，请在 Jarvis 应用内查看当前版本并检查更新。"
      />
    )
  }

  const line = updateStatusLine(status, update?.version || '')
  const description =
    status === 'check-failed'
      ? error
      : status === 'latest' && checkedAt
        ? `检查时间 ${dayjs(checkedAt).format('YYYY-MM-DD HH:mm:ss')}`
        : undefined
  const action =
    status === 'available'
      ? <Button size="small" type="primary" onClick={openPrompt}>查看并升级</Button>
      : status === 'install-failed'
        ? <Button size="small" onClick={openPrompt}>重新打开升级窗口</Button>
        : undefined

  return (
    <Flex vertical gap={16}>
      <Flex justify="space-between" align="flex-start" gap={16}>
        <div>
          <Title level={4} style={{ margin: '0 0 2px' }}>关于 Jarvis</Title>
          <Text type="secondary">桌面应用的版本与安装包更新；本机服务的运行参数在「运行」标签页。</Text>
        </div>
        <Button onClick={() => void check()} loading={status === 'checking'} disabled={status === 'installing'}>
          检查更新
        </Button>
      </Flex>
      <Descriptions column={1} size="small" bordered>
        <Descriptions.Item label="当前版本">{currentVersion || '读取中…'}</Descriptions.Item>
      </Descriptions>
      {line ? (
        line.tone === 'text'
          ? <Text type="secondary">{line.title}</Text>
          : <Alert type={line.tone} showIcon title={line.title} description={description} action={action} />
      ) : null}
    </Flex>
  )
}
