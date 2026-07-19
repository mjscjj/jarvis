import { StrictMode } from 'react'
import { createRoot } from 'react-dom/client'
import { ConfigProvider } from 'antd'
import zhCN from 'antd/locale/zh_CN'
import App from './App'
import './styles.css'

createRoot(document.getElementById('root')!).render(
  <StrictMode>
    <ConfigProvider
      locale={zhCN}
      theme={{
        token: {
          colorPrimary: '#244a3a',
          colorInfo: '#244a3a',
          colorBgBase: '#f3f5f2',
          colorTextBase: '#17241e',
          borderRadius: 10,
          fontFamily: 'Inter, "PingFang SC", "Helvetica Neue", sans-serif',
        },
      }}
    >
      <App />
    </ConfigProvider>
  </StrictMode>,
)
