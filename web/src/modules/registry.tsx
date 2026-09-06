import { lazy } from 'react'
import type { ComponentType, LazyExoticComponent, ReactNode } from 'react'
import { AimOutlined } from '@ant-design/icons'
import { OKR_TAB_DEFINITIONS } from '../okr/navigation'

export interface AppModulePageProps {
  moduleEnablement: Readonly<Record<string, boolean>>
}

export interface AppModuleChildDefinition {
  key: string
  label: string
  group?: string
  requiresModule?: string
  viewState: Record<string, string>
}

export interface AppModuleDefinition {
  key: string
  label: string
  icon: ReactNode
  Page: LazyExoticComponent<ComponentType<AppModulePageProps>>
  children?: readonly AppModuleChildDefinition[]
}

const BizOKRModule = lazy(() => import('../okr/OKRModule'))

// Code, routes and navigation metadata are registered here. The backend
// app-module catalog owns only enablement, so config cannot load arbitrary
// remote code or create a second plugin runtime.
export const appModuleRegistry: AppModuleDefinition[] = [
	{
		key: 'biz-okr',
		label: 'OKR',
		icon: <AimOutlined />,
		Page: BizOKRModule,
		children: OKR_TAB_DEFINITIONS.map((item) => ({
			key: item.key,
			label: item.label,
			group: item.group,
			requiresModule: 'requiresModule' in item ? item.requiresModule : undefined,
			viewState: { tab: item.key },
		})),
	},
]
