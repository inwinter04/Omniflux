export function schemaFromExample(value: unknown): Record<string, unknown> {
  if (value === null) return { type: 'null' };
  if (Array.isArray(value)) {
    return {
      type: 'array',
      items: value.length ? schemaFromExample(value[0]) : {}
    };
  }
  if (typeof value === 'object') {
    const entries = Object.entries(value);
    return {
      type: 'object',
      properties: Object.fromEntries(entries.map(([key, child]) => [key, schemaFromExample(child)])),
      required: entries.map(([key]) => key),
      additionalProperties: false
    };
  }
  if (typeof value === 'number') return { type: Number.isInteger(value) ? 'integer' : 'number' };
  if (typeof value === 'boolean') return { type: 'boolean' };
  return { type: 'string' };
}
