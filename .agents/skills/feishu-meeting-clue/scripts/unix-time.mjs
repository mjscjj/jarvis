// Mechanical conversion only: no host timezone or meeting-state assumptions.
import { pathToFileURL } from 'node:url'

export function formatUnixTime(seconds, timeZone) {
  if (!timeZone || !/^[0-9]+$/.test(seconds)) {
    throw new Error('An explicit timezone and Unix seconds string are required')
  }
  const millis = Number(seconds) * 1000
  if (!Number.isSafeInteger(millis)) throw new Error('Unix seconds are out of range')
  const date = new Date(millis)
  const formatter = new Intl.DateTimeFormat('en-US', {
    timeZone, year: 'numeric', month: '2-digit', day: '2-digit',
    hour: '2-digit', minute: '2-digit', second: '2-digit', hourCycle: 'h23',
    timeZoneName: 'longOffset',
  })
  const parts = Object.fromEntries(formatter.formatToParts(date).map(p => [p.type, p.value]))
  const offset = parts.timeZoneName === 'GMT' ? 'Z' : parts.timeZoneName.replace('GMT', '')
  return {
    unix_seconds: seconds,
    rfc3339: `${parts.year}-${parts.month}-${parts.day}T${parts.hour}:${parts.minute}:${parts.second}${offset}`,
  }
}

if (process.argv[1] && import.meta.url === pathToFileURL(process.argv[1]).href) {
  try {
    const [timeZone, ...seconds] = process.argv.slice(2)
    if (!timeZone || seconds.length === 0) {
      throw new Error('Usage: node unix-time.mjs TIMEZONE UNIX_SECONDS [UNIX_SECONDS ...]')
    }
    process.stdout.write(JSON.stringify(seconds.map(value => formatUnixTime(value, timeZone))) + '\n')
  } catch (error) {
    process.stderr.write(error.message + '\n')
    process.exitCode = 1
  }
}
