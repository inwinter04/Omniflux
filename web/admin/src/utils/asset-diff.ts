import { $t } from '@/locales';

type DiffOperation = {
  type: 'context' | 'add' | 'remove';
  text: string;
  oldIndex: number;
  newIndex: number;
};

function variableLines(spec: Record<string, unknown>) {
  const variables = Array.isArray(spec.variables) ? spec.variables : [];
  const lines = [$t('business.assetDiffVariableSettings')];
  if (variables.length === 0) lines.push($t('business.assetDiffNoVariables'));
  for (const raw of variables) {
    if (!raw || typeof raw !== 'object') continue;
    const variable = raw as Record<string, unknown>;
    const required = variable.required ? $t('common.yesOrNo.yes') : $t('common.yesOrNo.no');
    let line = `- ${String(variable.name ?? '')} | ${$t('business.assetDiffType')}: ${String(variable.type ?? '')} | ${$t('business.assetDiffRequired')}: ${required}`;
    if (variable.description)
      line += ` | ${$t('business.assetDiffDescription')}: ${String(variable.description).replace(/\n/g, ' ')}`;
    if (variable.default !== undefined)
      line += ` | ${$t('business.assetDiffDefaultValue')}: ${JSON.stringify(variable.default)}`;
    lines.push(line);
  }
  lines.push(
    '',
    $t('business.assetDiffModelService'),
    spec.model_service_id ? String(spec.model_service_id) : $t('business.assetDiffNotBound')
  );
  return lines;
}

function prettyPromptSpec(spec: Record<string, unknown>) {
  const template = String(spec.template ?? '');
  const lines = [
    $t('business.assetDiffTemplateContent'),
    template.endsWith('\n') ? template.slice(0, -1) : template,
    ''
  ];
  return [...lines, ...variableLines(spec)].join('\n');
}

function prettyAgentSpec(spec: Record<string, unknown>) {
  const lines = [
    $t('business.assetDiffAgentConfig'),
    `${$t('business.assetDiffPromptTemplate')}: ${String(spec.prompt_template_id ?? '')}`,
    `${$t('business.assetDiffPromptVersion')}: ${String(spec.prompt_version_id ?? '')}`,
    `${$t('business.assetDiffModelService')}: ${String(spec.model_service_id ?? '')}`,
    `${$t('business.assetDiffContextMessages')}: ${String(spec.max_history_messages ?? '')}`,
    `${$t('business.assetDiffMaxOutputTokens')}: ${String(spec.max_tokens ?? '')}`
  ];
  if (spec.temperature !== undefined && spec.temperature !== null) {
    lines.push(`${$t('business.assetDiffTemperature')}: ${Number(spec.temperature).toFixed(2)}`);
  }
  if (spec.top_p !== undefined && spec.top_p !== null) {
    lines.push(`${$t('business.assetDiffTopP')}: ${Number(spec.top_p).toFixed(2)}`);
  }
  return lines.join('\n');
}

function prettyAssetSpec(assetType: string, spec: Record<string, unknown>) {
  return assetType === 'agent' ? prettyAgentSpec(spec) : prettyPromptSpec(spec);
}

function diffLines(from: string[], to: string[]): DiffOperation[] {
  const table = Array.from({ length: from.length + 1 }, () => new Uint16Array(to.length + 1));
  for (let i = from.length - 1; i >= 0; i -= 1) {
    for (let j = to.length - 1; j >= 0; j -= 1) {
      table[i][j] = from[i] === to[j] ? table[i + 1][j + 1] + 1 : Math.max(table[i + 1][j], table[i][j + 1]);
    }
  }

  const operations: DiffOperation[] = [];
  let i = 0;
  let j = 0;
  while (i < from.length && j < to.length) {
    if (from[i] === to[j]) {
      operations.push({ type: 'context', text: from[i], oldIndex: i, newIndex: j });
      i += 1;
      j += 1;
    } else if (table[i + 1][j] >= table[i][j + 1]) {
      operations.push({ type: 'remove', text: from[i], oldIndex: i, newIndex: j });
      i += 1;
    } else {
      operations.push({ type: 'add', text: to[j], oldIndex: i, newIndex: j });
      j += 1;
    }
  }
  while (i < from.length) {
    operations.push({ type: 'remove', text: from[i], oldIndex: i, newIndex: j });
    i += 1;
  }
  while (j < to.length) {
    operations.push({ type: 'add', text: to[j], oldIndex: i, newIndex: j });
    j += 1;
  }
  return operations;
}

function unifiedDiff(operations: DiffOperation[]) {
  const changed = operations.map((item, index) => (item.type === 'context' ? -1 : index)).filter(index => index >= 0);
  if (changed.length === 0) {
    return `@@ -1,1 +1,1 @@\n ${$t('business.assetDiffNoChanges')}`;
  }

  const ranges: [number, number][] = [];
  for (const index of changed) {
    const start = Math.max(0, index - 3);
    const end = Math.min(operations.length - 1, index + 3);
    const last = ranges[ranges.length - 1];
    if (last && start <= last[1] + 1) last[1] = Math.max(last[1], end);
    else ranges.push([start, end]);
  }

  const chunks: string[] = [];
  for (const [start, end] of ranges) {
    const oldStart = operations[start].oldIndex + 1;
    const newStart = operations[start].newIndex + 1;
    let oldCount = 0;
    let newCount = 0;
    const lines: string[] = [];
    for (const item of operations.slice(start, end + 1)) {
      if (item.type !== 'add') oldCount += 1;
      if (item.type !== 'remove') newCount += 1;
      lines.push(`${item.type === 'add' ? '+' : item.type === 'remove' ? '-' : ' '}${item.text}`);
    }
    chunks.push(`@@ -${oldStart},${oldCount} +${newStart},${newCount} @@`, ...lines);
  }
  return chunks.join('\n');
}

export function buildAssetDiff(data: Api.Management.AssetDiff) {
  const fromText = prettyAssetSpec(data.asset_type, data.from_spec);
  const toText = prettyAssetSpec(data.asset_type, data.to_spec);
  const operations = diffLines(fromText.split('\n'), toText.split('\n'));
  const body = unifiedDiff(operations);
  return `--- v${data.from_version}\n+++ v${data.to_version}\n${body}\n`;
}
