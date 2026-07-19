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
          colorPrimary: '#166534',
          colorInfo: '#166534',
          colorSuccess: '#16a34a',
          colorWarning: '#d97706',
          colorError: '#dc2626',
          colorBgBase: '#fafaf9',
          colorTextBase: '#1c1917',
          colorBorder: '#e7e5e4',
          borderRadius: 10,
          borderRadiusLG: 14,
          fontFamily: 'Inter, "PingFang SC", "Helvetica Neue", "Microsoft YaHei", sans-serif',
        },
        components: {
          Card: {
            colorBgContainer: '#ffffff',
            boxShadow: '0 1px 2px rgba(0, 0, 0, 0.04), 0 4px 12px rgba(0, 0, 0, 0.05)',
            boxShadowTertiary: '0 1px 2px rgba(0, 0, 0, 0.04), 0 4px 12px rgba(0, 0, 0, 0.05)',
          },
          Menu: {
            itemBg: 'transparent',
            itemSelectedBg: '#dcfce7',
            itemSelectedColor: '#166534',
            itemColor: '#78716c',
            itemHoverColor: '#166534',
          },
          Tag: {
            borderRadiusSM: 6,
          },
          Button: {
            borderRadius: 8,
          },
        },
      }}
    >
      <App />
    </ConfigProvider>
  </StrictMode>,
)
