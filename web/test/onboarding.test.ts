import assert from 'node:assert/strict'
import test from 'node:test'
import { readFileSync } from 'node:fs'
import { setupAction, setupTaskStopped } from '../src/onboardingState.ts'
import { beginSetupLarkConnection, cancelSetupFlow, finalizeSetup } from '../src/api.ts'
import type { SetupStatus } from '../src/types.ts'

const ready = (): SetupStatus => ({
  runtime_id: 'test-runtime',
  configuration: { machine_configuration_ready: false, agent_name_configured: false },
  lark: { available: true, app_id: 'cli_test', bot: { status: 'ready', verified: true }, user: { status: 'ready', verified: true } },
  agent: { available: true, authenticated: true }, world_model_ready: false, completed: false,
})

test('only missing actions are shown; existing connections and login are skipped', () => {
  const status = ready()
  const saved = structuredClone(status)
  assert.equal(setupAction(status), 'start')
  assert.deepEqual(status, saved)
  status.agent.authenticated = false
  assert.equal(setupAction(status), 'agent')
  status.lark.user.verified = false
  assert.equal(setupAction(status), 'authorize')
  status.lark.bot.verified = false
  assert.equal(setupAction(status), 'repair')
  status.lark.app_id = ''
  assert.equal(setupAction(status), 'connect')
})

test('stopped initialization never leaves the retry button spinning', () => {
  for (const state of ['failed', 'observing', 'needs_human', 'done']) assert.equal(setupTaskStopped(state), true)
  for (const state of ['pending', 'executing', 'waiting']) assert.equal(setupTaskStopped(state), false)
})

test('secret editor lifecycle depends on edit mode and saved credential, not draft length', () => {
  const source = readFileSync(new URL('../src/Onboarding.tsx', import.meta.url), 'utf8')
  assert.match(source, /const secretEditorVisible = editingSecret \|\| \(!status\.configuration\.machine_configuration_ready && !status\.lark\.credential_available\)/)
  assert.doesNotMatch(source, /\{!appSecret\s*&&/)
  assert.match(source, /label htmlFor="setup-secret">App Secret/)
  assert.doesNotMatch(source, /<Steps|setup-app-id|setAppId|setSelected/)
  assert.doesNotMatch(source, /<details[^>]*\bopen[\s=>]/)
  assert.doesNotMatch(source, /localStorage\.setItem\([^\n]*[Ss]ecret/)
  assert.match(source, /无法读取安装状态/)
  assert.match(source, /https:\/\/open.feishu.cn\/app/)
})

test('frontend never chooses another App ID; only a missing secret is submitted', async (t) => {
  const calls: Array<{ path: string; body: unknown }> = []
  t.mock.method(globalThis, 'fetch', async (path: string, options: RequestInit) => {
    calls.push({ path, body: options.body ? JSON.parse(String(options.body)) : undefined })
    return new Response(JSON.stringify({ code: 0, data: {} }))
  })
  await beginSetupLarkConnection()
  await finalizeSetup('')
  await cancelSetupFlow('flow-1')
  assert.deepEqual(calls, [
    { path: '/api/setup/lark/connect', body: undefined },
    { path: '/api/setup/finalize', body: { app_secret: '' } },
    { path: '/api/setup/flows/flow-1/cancel', body: undefined },
  ])
})
