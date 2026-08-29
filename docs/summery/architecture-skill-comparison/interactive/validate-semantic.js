/* Static semantic checks for architecture.html. */
const fs = require('fs');
const path = require('path');

const htmlPath = path.resolve(process.argv[2] || 'architecture.html');
const html = fs.readFileSync(htmlPath, 'utf8');
const start = html.indexOf('const flows = {');
const end = html.indexOf('\n};\n\n/* ─── state', start) + 3;
if (start < 0 || end < 3) throw new Error('flows block not found');

const block = html.slice(start, end);
const flows = Function(`${block}\nreturn flows;`)();
const ids = new Set(Array.from(html.matchAll(/data-id="([^"]+)"/g), match => match[1]));
const tabs = Array.from(html.matchAll(/data-flow="([^"]+)"/g), match => match[1]);
const errors = [];

for (const [key, flow] of Object.entries(flows)) {
  if (!flow.name || !Array.isArray(flow.steps) || flow.steps.length < 3 || flow.steps.length > 9) {
    errors.push(`${key}: invalid name or step count`);
  }
  for (const [index, step] of flow.steps.entries()) {
    for (const field of ['from', 'to', 'color', 'title']) {
      if (!step[field]) errors.push(`${key}[${index + 1}]: missing ${field}`);
    }
    if (!ids.has(step.from) || !ids.has(step.to)) {
      errors.push(`${key}[${index + 1}]: missing node ${step.from}->${step.to}`);
    }
  }
}

const flowKeys = Object.keys(flows);
if (JSON.stringify(tabs) !== JSON.stringify(flowKeys)) errors.push(`tabs ${tabs} != flows ${flowKeys}`);
if (html.includes('{{')) errors.push('template placeholder remains');
if (errors.length) throw new Error(errors.join('\n'));

console.log(JSON.stringify({
  nodes: Array.from(ids),
  flows: Object.fromEntries(Object.entries(flows).map(([key, flow]) => [key, flow.steps.length])),
  tabs,
  placeholders: 0
}, null, 2));
