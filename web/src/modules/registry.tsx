import { lazy } from 'react'
import type { ComponentType, LazyExoticComponent, ReactNode } from 'react'
import { AimOutlined } from '@ant-design/icons'

export interface AppModuleDefinition {
  key: string
  label: string
  icon: ReactNode
  Page: LazyExoticComponent<ComponentType>
}

const OKRModule = lazy(() => import('../okr/OKRModule'))

// Code, routes and navigation metadata are registered here. The backend
// app-module catalog owns only enablement, so config cannot load arbitrary
// remote code or create a second plugin runtime.
export const appModuleRegistry: AppModuleDefinition[] = [
	{ key: 'okr', label: 'OKR', icon: <AimOutlined />, Page: OKRModule },
]
