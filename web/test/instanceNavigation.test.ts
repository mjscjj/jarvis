import assert from 'node:assert/strict'
import test from 'node:test'
import { okrEntryURL, preferredWorkbenchURL } from '../src/instanceNavigation.ts'

test('old public OKR links keep every query, share and comment parameter', () => {
  for (const route of ['/weekly-report', '/biz-okr', '/okr']) {
    const suffix = `?source=card#${route}?tab=okr-plan&quarter=2026-Q4&plan_id=plan-a&week=2026-W40&comment_id=comment-a&region=eu`
    assert.equal(okrEntryURL(`https://emily.example/${suffix}`, '/dev/'), `https://emily.example/dev/${suffix}`)
  }
})

test('local chat, task and scheduler pages stay with their instance', () => {
  for (const prefix of ['/', '/dev/']) {
    for (const route of ['/chat?session=abc', '/work/task/123', '/manage/automations', '/memory']) {
      assert.equal(okrEntryURL(`https://emily.example${prefix}#${route}`, '/dev/'), undefined)
    }
  }
})

test('disabled routing and an already selected installation never redirect', () => {
  assert.equal(okrEntryURL('https://emily.example/#/weekly-report?week=2026-W40', ''), undefined)
  for (const path of ['/dev/', '/dev']) {
    assert.equal(okrEntryURL(`https://emily.example${path}#/biz-okr`, '/dev/'), undefined)
  }
})


test('verified main preference changes non-OKR pages but never OKR or developer routing', () => {
  for (const hash of ['#/chat', '#/work', '#/manage/automations', '']) {
    const current = `https://emily.example/dev/${hash}`
    assert.equal(preferredWorkbenchURL(current, '/', true), `https://emily.example/${hash || '#/chat'}`)
    assert.equal(preferredWorkbenchURL(current, '/', false), undefined)
    assert.equal(preferredWorkbenchURL(current, '', true), undefined)
  }
  for (const hash of ['#/biz-okr?tab=okr-plan', '#/weekly-report', '#/okr']) {
    assert.equal(preferredWorkbenchURL(`https://emily.example/dev/${hash}`, '/', true), undefined)
  }
  assert.equal(preferredWorkbenchURL('https://emily.example/#/chat', '/', true), undefined)
})


test('explicit development object links never carry runtime IDs into the main instance', () => {
  for (const hash of ['#/chat?session=dev-session', '#/work/task/42', '#/manage/clues/42']) {
    assert.equal(preferredWorkbenchURL(`https://emily.example/dev/${hash}`, '/', true), undefined)
  }
})
