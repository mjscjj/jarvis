import assert from 'node:assert/strict'
import test from 'node:test'
import { setupAction, setupCanEnter, setupSecretVisible, worldModelProgress } from '../src/onboardingState.ts'
import { beginSetupLarkConnection, cancelSetupFlow, finalizeSetup, repairSetupLarkCredentials, rerunTask } from '../src/api.ts'
import type { SetupStatus, Task } from '../src/types.ts'

const ready = (): SetupStatus => ({
  runtime_id: 'test-runtime',
  app_ready: false,
  configuration: { machine_configuration_ready: false, agent_name_configured: false },
  lark: { available: true, app_id: 'cli_test', application_checks: [{ event: 'im.message.receive_v1', ready: true }, { event: 'card.action.trigger', ready: true }], bot: { status: 'ready', verified: true }, user: { status: 'ready', verified: true } },
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
  assert.equal(setupAction(status), 'application')
  status.lark.app_id = ''
  assert.equal(setupAction(status), 'connect')
})

test('app entry depends on usable runtime, not world model completion', () => {
  const status = ready()
  assert.equal(setupCanEnter(status, null), false)
  status.app_ready = true
  assert.equal(status.completed, false)
  assert.equal(status.world_model_ready, false)
  assert.equal(setupCanEnter(status, null), true)
  assert.equal(setupCanEnter(status, 'test-runtime'), false)
  assert.equal(setupCanEnter(status, 'previous-runtime'), true)
})

test('saved configuration after failed finalize cannot enter the old runtime', () => {
  const status = ready()
  status.configuration.machine_configuration_ready = true
  assert.equal(setupAction(status), 'start')
  assert.equal(setupCanEnter(status, null), false)
  assert.equal(setupCanEnter(status, status.runtime_id), false)
})

test('background progress shows recorded waiting reason, wake time and failure', () => {
  const task = { status: 'waiting', summary: '第一轮已写入人物', execution_result: { waiting: { reason: '等待文档权限生效', wake_at: '2026-09-10T12:00:00Z' } } } as unknown as Task
  const waiting = worldModelProgress(task)
  assert.match(waiting.title, /等待条件/)
  for (const detail of ['第一轮已写入人物', '等待文档权限生效', '2026-09-10T12:00:00Z']) assert.ok(waiting.detail.includes(detail))
  task.status = 'failed'
  task.execution_result = { error: 'Agent 调用超时', failure_reason: '文档正文尚未读完' }
  assert.match(worldModelProgress(task).title, /未完成/)
  assert.match(worldModelProgress(task).detail, /Agent 调用超时/)
  assert.match(worldModelProgress(task).detail, /文档正文尚未读完/)
  task.status = 'done'; task.summary = null; task.execution_result = null
  assert.match(worldModelProgress(task).detail, /未提供结果摘要/)
})

test('retry addresses the original task rather than creating initialization again', async (t) => {
  const calls: Array<{ path: string; method: string }> = []
  t.mock.method(globalThis, 'fetch', async (path: string, options: RequestInit) => {
    calls.push({ path, method: options.method! })
    return new Response(JSON.stringify({ code: 0, data: { task_id: 456, status: 'executing' } }))
  })
  await rerunTask(456)
  assert.deepEqual(calls, [{ path: '/api/tasks/456/rerun', method: 'POST' }])
})

test('a new user identifies the app and authorizes before entering its chat secret', () => {
  const status = ready()
  status.lark.user.verified = false
  assert.equal(setupAction(status), 'authorize')
  assert.equal(setupSecretVisible(status, false), false)
  status.lark.user.verified = true
  assert.equal(setupSecretVisible(status, false), true)
  status.lark.credential_available = true
  assert.equal(setupSecretVisible(status, false), false)
  assert.equal(setupSecretVisible(status, true), true)
  status.lark.app_id = ''
  assert.equal(setupSecretVisible(status, true), false)
})

test('event failures direct users to application configuration, never secret repair', () => {
  const status = ready()
  status.lark.application_checks[1] = { event: 'card.action.trigger', ready: false, error: 'console_event_published missing' }
  assert.equal(setupAction(status), 'application')
  assert.equal(setupSecretVisible(status, false), false)
  status.lark.application_checks[1].ready = true
  assert.equal(setupAction(status), 'start')
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
  await repairSetupLarkCredentials('new-secret')
  assert.deepEqual(calls, [
    { path: '/api/setup/lark/connect', body: undefined },
    { path: '/api/setup/finalize', body: { app_secret: '' } },
    { path: '/api/setup/flows/flow-1/cancel', body: undefined },
    { path: '/api/setup/lark/credentials', body: { app_secret: 'new-secret' } },
  ])
})
