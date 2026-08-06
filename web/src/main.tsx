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
          colorPrimary: '#287a4b',
          colorInfo: '#3b6fd4',
          colorSuccess: '#16a34a',
          colorWarning: '#b45309',
          colorError: '#dc2626',
          colorBgBase: '#ffffff',
          colorBgLayout: '#f7f8f9',
          colorTextBase: '#101418',
          colorTextSecondary: '#5b6470',
          colorBorder: '#e8eaed',
          colorBorderSecondary: '#eef0f2',
          borderRadius: 8,
          borderRadiusLG: 12,
          borderRadiusSM: 6,
          controlHeight: 34,
          fontSize: 14,
          fontFamily: '"Inter", "PingFang SC", "Helvetica Neue", "Microsoft YaHei", sans-serif',
          // 卡片/表格默认无阴影，靠细边框；阴影只留浮层
          boxShadow: '0 4px 16px rgba(16, 20, 24, 0.08)',
          boxShadowSecondary: '0 12px 40px rgba(16, 20, 24, 0.12)',
        },
        components: {
          Card: {
            colorBgContainer: '#ffffff',
            boxShadow: 'none',
            boxShadowTertiary: 'none',
          },
          Menu: {
            itemBg: 'transparent',
            itemSelectedBg: '#e8f3ec',
            itemSelectedColor: '#287a4b',
            itemColor: '#5b6470',
            itemHoverColor: '#287a4b',
            itemHoverBg: '#f2f3f5',
            itemHeight: 38,
          },
          Table: {
            headerBg: '#f7f8f9',
            headerColor: '#5b6470',
            headerSplitColor: 'transparent',
            borderColor: '#eef0f2',
            rowHoverBg: '#f7f8f9',
            cellPaddingBlock: 12,
          },
          Tag: {
            borderRadiusSM: 6,
          },
          Button: {
            borderRadius: 8,
            fontWeight: 500,
          },
          Segmented: {
            borderRadius: 8,
            itemSelectedColor: '#287a4b',
          },
        },
      }}
    >
      <App />
    </ConfigProvider>
  </StrictMode>,
)
