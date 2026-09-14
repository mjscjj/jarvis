import assert from 'node:assert/strict'
import test from 'node:test'
import { appPath } from '../src/appPath.ts'

test('deployment prefixes keep APIs and attachments on their installation', () => {
  for (const base of ['/', '/dev/', '/sandbox/emily/']) {
    assert.equal(appPath('/api/tasks', base), `${base}api/tasks`)
    assert.equal(appPath('/okr-assets/a.png', base), `${base}okr-assets/a.png`)
    assert.equal(appPath(`${base}api/tasks`, base), `${base}api/tasks`)
    assert.equal(appPath('https://example.com/doc', base), 'https://example.com/doc')
    assert.equal(appPath('//example.com/image', base), '//example.com/image')
  }
})
